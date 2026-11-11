//go:build !windows

package localdb

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestNewDatabaseIsCreatedWith0640(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	path := filepath.Join(t.TempDir(), "n", "notes.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("mode %o", fi.Mode().Perm())
	}
	if w := ModeWarning(path); w != "" {
		t.Errorf("unexpected warning %q", w)
	}
}

func TestOtherModeIsKeptAndWarnedAbout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("an existing file's mode must be left alone, got %o", fi.Mode().Perm())
	}
	w := ModeWarning(path)
	if !strings.Contains(w, "0644") || !strings.Contains(w, "0640") || !strings.Contains(w, "chmod 640") {
		t.Errorf("warning %q", w)
	}
	if ModeWarning(filepath.Join(t.TempDir(), "none")) != "" {
		t.Error("no file, no warning")
	}
}
