// Package remote is the optional second write: sending notes that are in the
// local database to the trecs server, and recording how that went on each
// note (see localdb.SetRemoteSync).
package remote

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"os"

	"github.com/solworktech/trecs-notes/internal/api"
	"github.com/solworktech/trecs-notes/internal/config"
	"github.com/solworktech/trecs-notes/internal/credentials"
	"github.com/solworktech/trecs-notes/internal/instance"
	"github.com/solworktech/trecs-notes/internal/localdb"
	"github.com/solworktech/trecs-notes/internal/model"
)

// syncMu lets one send or one pull at a time run in this process: the
// background sync after a save, the timed one and a button press must not
// send the same new note twice.
var syncMu sync.Mutex

// lockSync takes syncMu and a lock on sync.lock in the data folder, which
// does the same across processes (the tray, a notes window and a capture
// are separate processes). If the file lock cannot be had - it is busy for
// 30 seconds, or the folder is unusable - the in-process lock alone is kept
// and a busy lock is reported as an error.
func lockSync() (unlock func(), err error) {
	syncMu.Lock()
	dir, derr := config.NotesDir()
	if derr != nil {
		return syncMu.Unlock, nil
	}
	if derr := os.MkdirAll(dir, 0o700); derr != nil {
		return syncMu.Unlock, nil
	}
	l, lerr := instance.WaitLock(filepath.Join(dir, "sync.lock"), 30*time.Second)
	if lerr != nil {
		if errors.Is(lerr, instance.ErrHeld) {
			syncMu.Unlock()
			return nil, errors.New("another trecs-notes is syncing; try again in a moment")
		}
		return syncMu.Unlock, nil
	}
	return func() { l.Unlock(); syncMu.Unlock() }, nil
}

// withClient runs fn with a client for the saved login. If the session is
// missing or has lapsed and a login is stored (internal/credentials), it
// logs in again once and retries. Credentials only ever go to the server of
// the saved login.
func withClient(fn func(context.Context, *api.Client) error) error {
	run := func() error {
		sess, err := api.LoadSession()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return fn(ctx, api.FromSession(sess))
	}
	err := run()
	if errors.Is(err, api.ErrNotLoggedIn) && relogin() == nil {
		err = run()
	}
	return err
}

func request(n model.Note) api.NoteRequest {
	return api.NoteRequest{
		AppName: n.AppName, WindowTitle: n.WindowTitle, Body: n.Text,
		Tags: n.Tags, CreatedAt: n.CreatedAtEpoch(), UpdatedAt: n.UpdatedAtEpoch(),
	}
}

// SyncNote sends a note the server does not have yet and records the
// outcome on it, success or failure.
//
// It waits for any other sync to finish first, and then looks at the note
// again: if that sync already sent it, nothing is sent twice.
func SyncNote(db *localdb.DB, n model.Note) error {
	unlock, err := lockSync()
	if err != nil {
		return err
	}
	defer unlock()
	if cur, gerr := db.Get(n.ID); gerr == nil && cur.RemoteID != "" {
		return nil
	}
	return syncNote(db, n)
}

func syncNote(db *localdb.DB, n model.Note) error {
	var id string
	err := withClient(func(ctx context.Context, c *api.Client) (err error) {
		id, err = c.CreateNote(ctx, request(n))
		return err
	})
	if err != nil {
		_ = db.SetRemoteSync(n.ID, "", err.Error())
		return err
	}
	return db.SetRemoteSync(n.ID, id, "")
}

// SyncEdit sends an edited note to the server. If the server has lost it,
// the note is sent again as a new one.
func SyncEdit(db *localdb.DB, n model.Note) error {
	unlock, err := lockSync()
	if err != nil {
		return err
	}
	defer unlock()
	return syncEdit(db, n)
}

func syncEdit(db *localdb.DB, n model.Note) error {
	err := withClient(func(ctx context.Context, c *api.Client) error {
		return c.UpdateNote(ctx, n.RemoteID, request(n))
	})
	if errors.Is(err, api.ErrRemoteGone) {
		if e := db.ForgetRemote(n.ID); e != nil {
			return e
		}
		n.RemoteID = ""
		return syncNote(db, n)
	}
	if err != nil {
		_ = db.SetRemoteSync(n.ID, n.RemoteID, err.Error())
		return err
	}
	return db.SetRemoteSync(n.ID, n.RemoteID, "")
}

// SyncDelete tells the server a note was deleted here, then forgets the
// tombstone.
func SyncDelete(db *localdb.DB, remoteID string) error {
	unlock, err := lockSync()
	if err != nil {
		return err
	}
	defer unlock()
	return syncDelete(db, remoteID)
}

func syncDelete(db *localdb.DB, remoteID string) error {
	err := withClient(func(ctx context.Context, c *api.Client) error {
		return c.DeleteNote(ctx, remoteID)
	})
	if err != nil {
		return err
	}
	return db.ClearTombstone(remoteID)
}

// SyncPending sends everything the server has not seen, in the background
// where a frontend wants that: edits and deletions as well as new notes.
func SyncPending(db *localdb.DB) {
	_, _, _ = SendUnsynced(db, 100)
}

// PullResult says what a pull changed here.
type PullResult = localdb.PullStats

// Pull takes in what changed on the server since the last pull: new notes,
// edits and deletions made on the website or by trecs-notes on another
// machine. Merging rules are localdb.ApplyRemote's. It keeps its place
// (per server) after every page, so an interrupted pull carries on where it
// stopped.
func Pull(db *localdb.DB) (PullResult, error) {
	unlock, err := lockSync()
	if err != nil {
		return PullResult{}, err
	}
	defer unlock()
	var total PullResult
	for {
		more := false
		err := withClient(func(ctx context.Context, c *api.Client) error {
			cursor, err := db.Cursor(c.Server)
			if err != nil {
				return err
			}
			page, err := c.NoteChangesSince(ctx, cursor, 200)
			if err != nil {
				return err
			}
			for _, ch := range page.Changes {
				switch {
				case ch.Note != nil:
					n := ch.Note
					err = db.ApplyRemote(localdb.RemoteNote{
						ID: n.ID, AppName: n.AppName, WindowTitle: n.WindowTitle, Text: n.Body, Tags: n.Tags,
						CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt,
					}, &total)
				case ch.Deleted != "":
					err = db.ApplyRemoteDelete(ch.Deleted, &total)
				}
				if err != nil {
					return err
				}
			}
			more = page.More && len(page.Changes) > 0
			if page.Cursor != cursor {
				return db.SetCursor(c.Server, page.Cursor)
			}
			return nil
		})
		if err != nil || !more {
			return total, err
		}
	}
}

// Sync sends what the server has not seen, then takes in what it has that
// this database has not: both directions. The pull is skipped when the
// push found nobody logged in.
func Sync(db *localdb.DB) (sent, failed int, pulled PullResult, lastErr error) {
	sent, failed, lastErr = SendUnsynced(db, 500)
	if errors.Is(lastErr, api.ErrNotLoggedIn) {
		return sent, failed, pulled, lastErr
	}
	pulled, perr := Pull(db)
	if perr != nil {
		lastErr = fmt.Errorf("fetching changes: %w", perr)
		failed++
	}
	return sent, failed, pulled, lastErr
}

// DescribeSync is Sync's result as one line for a person.
func DescribeSync(sent, failed int, pulled PullResult, lastErr error) string {
	var parts []string
	if sent > 0 {
		parts = append(parts, fmt.Sprintf("Sent %d.", sent))
	}
	if n := pulled.Added + pulled.Updated + pulled.Removed; n > 0 {
		var got []string
		if pulled.Added > 0 {
			got = append(got, fmt.Sprintf("%d new", pulled.Added))
		}
		if pulled.Updated > 0 {
			got = append(got, fmt.Sprintf("%d changed", pulled.Updated))
		}
		if pulled.Removed > 0 {
			got = append(got, fmt.Sprintf("%d removed", pulled.Removed))
		}
		parts = append(parts, "Received "+strings.Join(got, ", ")+".")
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed: %v", failed, lastErr))
	}
	if len(parts) == 0 {
		return "Everything is up to date."
	}
	return strings.Join(parts, " ")
}

// relogin renews the session from the stored login.
func relogin() error {
	l, ok := credentials.Load()
	if !ok || l.Email == "" || l.Password == "" || l.Server == "" {
		return api.ErrNotLoggedIn
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sess, err := api.Login(ctx, l.Server, l.Email, l.Password)
	if err != nil {
		return err
	}
	return sess.Save()
}

// SendUnsynced sends every change the server has not seen - new notes
// (oldest first), edits and deletions, up to limit of each - and says how
// many went and how many did not. It stops at the first "not logged in",
// since the rest would fail the same way.
func SendUnsynced(db *localdb.DB, limit int) (sent, failed int, lastErr error) {
	unlock, err := lockSync()
	if err != nil {
		return 0, 0, err
	}
	defer unlock()
	fresh, err := db.Unsynced(limit)
	if err != nil {
		return 0, 0, err
	}
	edited, err := db.Dirty(limit)
	if err != nil {
		return 0, 0, err
	}
	gone, err := db.Tombstones()
	if err != nil {
		return 0, 0, err
	}
	total := len(fresh) + len(edited) + len(gone)
	do := func(f func() error) (stop bool) {
		if err := f(); err != nil {
			failed++
			lastErr = err
			if errors.Is(err, api.ErrNotLoggedIn) {
				failed = total - sent
				return true
			}
			return false
		}
		sent++
		return false
	}
	for _, n := range fresh {
		if do(func() error { return syncNote(db, n) }) {
			return sent, failed, lastErr
		}
	}
	for _, n := range edited {
		if do(func() error { return syncEdit(db, n) }) {
			return sent, failed, lastErr
		}
	}
	for _, id := range gone {
		if do(func() error { return syncDelete(db, id) }) {
			return sent, failed, lastErr
		}
	}
	return sent, failed, lastErr
}

// Describe turns SendUnsynced's result into one line for a person.
func Describe(sent, failed int, lastErr error) string {
	switch {
	case sent == 0 && failed == 0:
		return "Nothing to send."
	case failed == 0:
		return fmt.Sprintf("Sent %d.", sent)
	default:
		return fmt.Sprintf("Sent %d, %d failed: %v", sent, failed, lastErr)
	}
}
