// Package remote is the optional second write: sending notes that are in the
// local database to the trecs server, and recording how that went on each
// note (see localdb.SetRemoteSync).
package remote

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/solworktech/trecs-notes/internal/api"
	"github.com/solworktech/trecs-notes/internal/credentials"
	"github.com/solworktech/trecs-notes/internal/localdb"
	"github.com/solworktech/trecs-notes/internal/model"
)

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
		Tags: n.Tags, CreatedAt: n.CreatedAtEpoch(),
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
