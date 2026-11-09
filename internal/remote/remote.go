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

// SyncNote sends one note and records the outcome on it, success or failure.
// The note is sent to the server of the saved login: credentials never go
// anywhere else. If the session is missing or has lapsed and a login is
// stored (internal/credentials), it logs in again once and retries.
func SyncNote(db *localdb.DB, n model.Note) error {
	id, err := send(n)
	if errors.Is(err, api.ErrNotLoggedIn) && relogin() == nil {
		id, err = send(n)
	}
	if err != nil {
		_ = db.SetRemoteSync(n.ID, "", err.Error())
		return err
	}
	return db.SetRemoteSync(n.ID, id, "")
}

func send(n model.Note) (string, error) {
	sess, err := api.LoadSession()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return api.FromSession(sess).CreateNote(ctx, api.NoteRequest{
		AppName: n.AppName, WindowTitle: n.WindowTitle, Body: n.Text,
		Tags: n.Tags, CreatedAt: n.CreatedAtEpoch(),
	})
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

// SendUnsynced sends every note the server does not have yet (oldest first,
// up to limit) and says how many went and how many did not. It stops at the
// first "not logged in", since the rest would fail the same way.
func SendUnsynced(db *localdb.DB, limit int) (sent, failed int, lastErr error) {
	notes, err := db.Unsynced(limit)
	if err != nil {
		return 0, 0, err
	}
	for _, n := range notes {
		if err := SyncNote(db, n); err != nil {
			failed++
			lastErr = err
			if errors.Is(err, api.ErrNotLoggedIn) {
				failed = len(notes) - sent
				break
			}
			continue
		}
		sent++
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
