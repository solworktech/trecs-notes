//go:build darwin

package capture

import (
	"bytes"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// capture is the macOS implementation, all through osascript.
//
// The selection is read from the Accessibility API: the focused UI element's
// AXSelectedText attribute, reached through System Events. That sends no
// keystroke and leaves the clipboard alone. It works in native Cocoa apps and
// in applications that expose accessibility (Safari, Terminal, most others);
// Chromium and Electron applications only do so once an assistive client is
// seen, so the first capture in one may come back empty. Like all use of
// System Events it needs the Accessibility permission, under System Settings >
// Privacy & Security > Accessibility; macOS asks for it the first time.
//
// With Options.CopyFallback on, an application that gave no selection is sent
// Cmd+C (a copy in every macOS application, terminals included, so unlike
// Ctrl+C elsewhere it cannot interrupt anything). The result is trusted only
// if the pasteboard's change count moved, and the previous text is put back;
// if the pasteboard holds anything but plain text, nothing is sent.
//
// An application's Services menu is the one real system-wide "selected text"
// API macOS has; using it needs an application bundle, so it belongs with the
// packaging work.
func capture(opts Options) Snapshot {
	var s Snapshot

	const script = `
tell application "System Events"
	set frontApp to first application process whose frontmost is true
	set appName to name of frontApp
	set winTitle to ""
	try
		set winTitle to name of front window of frontApp
	end try
	set sel to ""
	try
		set sel to value of attribute "AXSelectedText" of (value of attribute "AXFocusedUIElement" of frontApp)
	end try
	if winTitle is missing value then set winTitle to ""
	if sel is missing value then set sel to ""
	return appName & linefeed & winTitle & linefeed & sel
end tell`
	out, err := runOut("osascript", "-e", script)
	if err != nil {
		s.Partial = true
		s.Warnings = append(s.Warnings, "osascript (frontmost app, window, selection): "+err.Error()+
			" - System Events may not have Accessibility permission yet (System Settings > Privacy & Security > Accessibility)")
		return s
	}
	out = strings.TrimSuffix(out, "\n") // osascript ends its output with one newline
	parts := strings.SplitN(out, "\n", 3)
	s.AppName = strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		s.WindowTitle = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		s.SelectedText = parts[2]
	}
	if s.SelectedText != "" {
		return s
	}

	s.Partial = true
	switch {
	case !opts.CopyFallback:
		s.Warnings = append(s.Warnings, "nothing selected, or this application does not report its selection (the copy fallback is off in Settings)")
	default:
		text, warn := macCopyFallback()
		s.SelectedText = text
		if warn != "" {
			s.Warnings = append(s.Warnings, warn)
		}
	}
	return s
}

// pasteboardState returns the general pasteboard's change count and whether
// every type on it is plain text.
func pasteboardState() (count int, textOnly bool, err error) {
	const js = `ObjC.import('AppKit');
var pb = $.NSPasteboard.generalPasteboard;
var types = ObjC.deepUnwrap(pb.types) || [];
pb.changeCount + "\n" + types.join("\n")`
	out, err := runOut("osascript", "-l", "JavaScript", "-e", js)
	if err != nil {
		return 0, false, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	count, err = strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return 0, false, err
	}
	textOnly = true
	for _, t := range lines[1:] {
		t = strings.TrimSpace(t)
		switch {
		case t == "", t == "public.utf8-plain-text", t == "public.plain-text", t == "NSStringPboardType",
			t == "public.utf16-plain-text", t == "public.utf16-external-plain-text",
			strings.HasPrefix(t, "org.nspasteboard."), strings.HasPrefix(t, "dyn."):
		default:
			textOnly = false
		}
	}
	return count, textOnly, nil
}

func macCopyFallback() (text, warning string) {
	count, plain, err := pasteboardState()
	if err != nil {
		return "", "could not inspect the pasteboard: " + err.Error()
	}
	if !plain {
		return "", "the clipboard holds something other than text, which cannot be put back after a copy: not copying"
	}
	before, _ := runOut("pbpaste")

	const copyScript = `tell application "System Events" to keystroke "c" using command down`
	if _, err := runOut("osascript", "-e", copyScript); err != nil {
		return "", "osascript (send Cmd+C): " + err.Error()
	}
	changed := false
	for i := 0; i < 25 && !changed; i++ { // up to ~500 ms for the application to answer
		time.Sleep(20 * time.Millisecond)
		if c, _, err := pasteboardState(); err == nil && c != count {
			changed = true
		}
	}
	if !changed {
		return "", "nothing selected (the copy did not change the clipboard)"
	}
	text, err = runOut("pbpaste")
	if err != nil {
		return "", "pbpaste: " + err.Error()
	}
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(before)
	if err := cmd.Run(); err != nil {
		return text, "the selection was copied but the previous clipboard text could not be restored: " + err.Error()
	}
	return text, ""
}

func runOut(name string, args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}
