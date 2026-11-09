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
	"os"
	"path/filepath"

	"github.com/solworktech/trecs-notes/internal/config"
	"github.com/zalando/go-keyring"
)

const (
	service = "trecs-notes"
	account = "login"
)

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
	if kerr := keyring.Set(service, account, string(data)); kerr == nil {
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
	if s, err := keyring.Get(service, account); err == nil {
		if json.Unmarshal([]byte(s), &l) == nil {
			return l, true
		}
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
	if err := keyring.Delete(service, account); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		_ = err // no keyring running: nothing to delete there
	}
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
