// Package remote is the optional second write: sending notes that are in the
// local database to the trecs server, and recording how that went on each
// note (see localdb.SetRemoteSync).
package remote

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/solworktech/trecs-notes/internal/api"
	"github.com/solworktech/trecs-notes/internal/credentials"
	"github.com/solworktech/trecs-notes/internal/localdb"
	"github.com/solworktech/trecs-notes/internal/model"
)

// syncMu lets one send or one pull at a time run in this process: the
// background sync after a save, the timed one and a button press must not
// send the same new note twice.
var syncMu sync.Mutex

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
func SyncNote(db *localdb.DB, n model.Note) error {
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
	err := withClient(func(ctx context.Context, c *api.Client) error {
		return c.UpdateNote(ctx, n.RemoteID, request(n))
	})
	if errors.Is(err, api.ErrRemoteGone) {
		if e := db.ForgetRemote(n.ID); e != nil {
			return e
		}
		n.RemoteID = ""
		return SyncNote(db, n)
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
	syncMu.Lock()
	defer syncMu.Unlock()
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
	syncMu.Lock()
	defer syncMu.Unlock()
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
		if do(func() error { return SyncNote(db, n) }) {
			return sent, failed, lastErr
		}
	}
	for _, n := range edited {
		if do(func() error { return SyncEdit(db, n) }) {
			return sent, failed, lastErr
		}
	}
	for _, id := range gone {
		if do(func() error { return SyncDelete(db, id) }) {
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
