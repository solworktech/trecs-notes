//go:build linux

package hotkey

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// Needs an X display and xdotool; skipped without (as on a CI runner). Not an
// F key: with the usual keymaps the X server itself takes Ctrl+Alt+F1..F12
// (switching virtual terminals) and nothing can grab them.
func TestRegisterFiresAndConflicts(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("no X display")
	}
	if _, err := exec.LookPath("xdotool"); err != nil {
		t.Skip("no xdotool")
	}
	fired := make(chan struct{}, 1)
	h, err := Register("Ctrl+Alt+Q", func() { fired <- struct{}{} })
	if err != nil {
		t.Skipf("cannot grab on this display: %v", err)
	}
	defer func() { _ = h.Unregister() }()
	if _, err := Register("Ctrl+Alt+Q", func() {}); err == nil {
		t.Error("a second grab of the same combination should fail")
	}
	if err := exec.Command("xdotool", "key", "ctrl+alt+q").Run(); err != nil {
		t.Skipf("xdotool: %v", err)
	}
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Error("hotkey did not fire")
	}
}
