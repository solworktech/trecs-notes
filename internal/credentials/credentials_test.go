package credentials

import (
	"errors"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

// These tests never touch the machine's real keyring: on a macOS runner that
// means a keychain prompt nobody can answer.

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
}

func TestStoredInKeyring(t *testing.T) {
	isolate(t)
	keyring.MockInit()
	in := Login{Server: "https://x.example", Email: "a@b.c", Password: "pw"}
	where, err := Save(in)
	if err != nil || where != "the system keyring" {
		t.Fatalf("Save: %q, %v", where, err)
	}
	if p, _ := filePath(); fileExists(p) {
		t.Error("a plain-text copy was written although the keyring worked")
	}
	if got, ok := Load(); !ok || got != in {
		t.Fatalf("Load: %+v ok=%v", got, ok)
	}
	Delete()
	if _, ok := Load(); ok {
		t.Fatal("still there after Delete")
	}
}

func TestFileFallbackWithoutKeyring(t *testing.T) {
	isolate(t)
	keyring.MockInitWithError(errors.New("no keyring running"))
	in := Login{Server: "https://x.example", Email: "a@b.c", Password: "pw"}
	if _, err := Save(in); err != nil {
		t.Fatal(err)
	}
	p, _ := filePath()
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("credentials file: %v, %v (want mode 0600)", fi, err)
		}
	}
	if got, ok := Load(); !ok || got != in {
		t.Fatalf("Load: %+v ok=%v", got, ok)
	}
	Delete()
	if _, ok := Load(); ok || fileExists(p) {
		t.Fatal("still there after Delete")
	}
}

func TestWithTimeoutGivesUp(t *testing.T) {
	start := time.Now()
	err := withTimeout(50*time.Millisecond, func() error { time.Sleep(5 * time.Second); return nil })
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("did not give up in time: %v after %v", err, time.Since(start))
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
