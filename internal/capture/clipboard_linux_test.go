//go:build linux

package capture

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestTextOnly(t *testing.T) {
	for _, c := range []struct {
		targets []string
		want    bool
	}{
		{[]string{"TARGETS", "TIMESTAMP", "UTF8_STRING", "STRING", "TEXT", "text/plain", "text/plain;charset=utf-8"}, true},
		{[]string{"UTF8_STRING", "image/png"}, false},
		{[]string{"text/uri-list", "UTF8_STRING"}, false},
		{[]string{"text/html", "UTF8_STRING"}, false},
		{nil, true},
	} {
		if got := textOnly(c.targets); got != c.want {
			t.Errorf("textOnly(%v) = %v, want %v", c.targets, got, c.want)
		}
	}
}

// The restore half of the copy fallback: what writeClipboard puts back is
// what readClipboard reads, and an empty text clears it.
func TestWriteClipboardRoundTrip(t *testing.T) {
	if os.Getenv("DISPLAY") == "" || !commandExists("xclip") {
		t.Skip("needs an X display and xclip")
	}
	if err := writeClipboard(false, "previous text\nsecond line"); err != nil {
		t.Fatal(err)
	}
	if got, _ := readClipboard(false); got != "previous text\nsecond line" {
		t.Errorf("read back %q", got)
	}
	targets, known := clipboardTargets(false)
	if !known || !textOnly(targets) {
		t.Errorf("targets %v known=%v: text written by xclip should be text-only", targets, known)
	}
}

// The default path end to end: text in the PRIMARY selection is what a
// capture returns, with no keystroke and the clipboard untouched.
func TestCapturePrimaryLeavesClipboardAlone(t *testing.T) {
	if os.Getenv("DISPLAY") == "" || !commandExists("xclip") {
		t.Skip("needs an X display and xclip")
	}
	if err := writeClipboard(false, "clipboard stays"); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("xclip", "-selection", "primary", "-i")
	cmd.Stdin = strings.NewReader("highlighted text")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	snap := Capture(Options{})
	if snap.SelectedText != "highlighted text" {
		t.Errorf("SelectedText = %q (warnings: %v)", snap.SelectedText, snap.Warnings)
	}
	if got, _ := readClipboard(false); got != "clipboard stays" {
		t.Errorf("the clipboard changed to %q", got)
	}
}
