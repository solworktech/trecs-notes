// Package credentials keeps the trecs login (server, email, password) so
// the person does not retype it, and so a lapsed session can be renewed
// without bothering them.
//
// The password goes to the operating system's own secret store when there
// is one (macOS Keychain, Windows Credential Manager, the freedesktop Secret
// Service - GNOME Keyring or KWallet - on Linux). If none is running, it
// falls back to a file readable only by the current user in the notes
// folder; that is plain text on disk, protected by file permissions alone,
// and Where() says which of the two is in use so the UI can tell the person.
package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/solworktech/trecs-notes/internal/config"
	"github.com/zalando/go-keyring"
)

const (
	service = "trecs-notes"
	account = "login"
)

// keyringTimeout bounds every keyring call. A keyring can wait for a person
// to answer a prompt (a locked macOS keychain, a Secret Service asking for
// the login password) and a program with no one to answer would otherwise
// wait forever: a CI runner hung for ten minutes on exactly that. On timeout
// the call counts as failed and the file fallback is used.
var keyringTimeout = 10 * time.Second

func withTimeout(d time.Duration, fn func() error) error {
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(d):
		return fmt.Errorf("the keyring did not answer within %s", d)
	}
}

// Login is the stored login.
type Login struct {
	Server   string `json:"server"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func filePath() (string, error) {
	dir, err := config.NotesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials.json"), nil
}

// Save stores the login; where is "keyring" or the fallback file's path.
func Save(l Login) (where string, err error) {
	data, err := json.Marshal(l)
	if err != nil {
		return "", err
	}
	kerr := withTimeout(keyringTimeout, func() error { return keyring.Set(service, account, string(data)) })
	if kerr == nil {
		_ = removeFile() // never leave a stale plain-text copy behind
		return "the system keyring", nil
	}
	p, err := filePath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, p); err != nil {
		return "", err
	}
	return p + " (no system keyring found; plain text, readable only by you)", nil
}

// Load returns the stored login, if any.
func Load() (Login, bool) {
	var l Login
	var s string
	err := withTimeout(keyringTimeout, func() (err error) { s, err = keyring.Get(service, account); return })
	if err == nil && json.Unmarshal([]byte(s), &l) == nil {
		return l, true
	}
	if p, err := filePath(); err == nil {
		if data, err := os.ReadFile(p); err == nil && json.Unmarshal(data, &l) == nil {
			return l, true
		}
	}
	return Login{}, false
}

// Delete forgets the stored login everywhere.
func Delete() {
	// no keyring running, or nothing stored there: nothing to delete
	_ = withTimeout(keyringTimeout, func() error { return keyring.Delete(service, account) })
	_ = removeFile()
}

func removeFile() error {
	p, err := filePath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
