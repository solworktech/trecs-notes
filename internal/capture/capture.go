// Package capture answers one question when the hotkey fires: what app was
// focused, what was its window titled, and what text was selected?
//
// There is no operating-system call that returns "the text selected in
// whatever window has focus" on every platform, so each one is asked in the
// way it offers, and nothing is typed into the person's windows unless they
// have switched that on:
//
//   - Linux: the PRIMARY selection, which holds whatever is highlighted in
//     nearly any GTK, Qt, Firefox or terminal window, with no keystroke.
//     Read with xclip, xsel or wl-paste.
//   - Windows: UI Automation's TextPattern, the interface screen readers use,
//     asked for the focused element's selection (PowerShell, no keystroke).
//   - macOS: the Accessibility attribute AXSelectedText of the focused
//     element (AppleScript through System Events, no keystroke). It needs
//     the Accessibility permission that System Events asks for anyway.
//
// None of these is universal: some applications do not expose their
// selection. Options.CopyFallback, off by default, lets the capture then send
// the copy shortcut as a last resort. It never does so into a terminal (where
// Ctrl+C means "interrupt"), only trusts the result if the clipboard really
// changed, and puts the previous clipboard text back; if the clipboard holds
// anything but plain text (an image, files, formatted text) it does not try,
// since that could not be put back faithfully.
//
// Helper tools are used rather than Go bindings (no cgo, no per-OS system
// call layer to keep in step); each OS ships what it needs, except Linux,
// which needs one of xclip, xsel or wl-clipboard. See Snapshot.Partial for
// what happens when something cannot be read.
package capture

import "strings"

// Snapshot is what was captured, best-effort. A zero-value field (AppName,
// WindowTitle or SelectedText all empty) is not an error - some apps and
// some window managers (Wayland compositors especially; see the package
// doc) don't expose this, and Electron apps and some terminal emulators are
// known to swallow a synthetic copy keystroke. The caller (internal/ui)
// shows blank fields for the person to fill in by hand, per the agreed
// fallback.
type Snapshot struct {
	AppName      string
	WindowTitle  string
	SelectedText string

	// Partial is true if any field could not be read and was left blank
	// rather than erroring out the whole capture. Warnings holds a short
	// reason per field, for a debug log - not shown to the person; a blank
	// field in the dialog already tells them to fill it in.
	Partial  bool
	Warnings []string
}

// Options are the person's choices about how a capture may behave.
type Options struct {
	// CopyFallback allows sending the copy shortcut to an application that
	// does not report its selection (see the package doc for the limits).
	CopyFallback bool
}

// Capture takes the snapshot. Implemented per-OS (capture_linux.go,
// capture_darwin.go, capture_windows.go); each builds a Snapshot field by
// field rather than failing the whole call when one part isn't available.
func Capture(opts Options) Snapshot {
	return capture(opts)
}

// terminals are applications where Ctrl+C means "interrupt the running
// program", not "copy": the copy fallback is never sent to them. Names are
// compared lower-case; the longer ones as a substring (so "gnome-terminal"
// and "WindowsTerminal" match), the short ones only as the whole name or a
// prefix followed by a separator, so "rio" does not match "Mario".
var terminals = []string{
	"wezterm", "xterm", "kitty", "alacritty", "foot", "konsole", "gnome-terminal", "terminator",
	"tilix", "st-256color", "st", "urxvt", "xfce4-terminal", "mate-terminal", "lxterminal", "ghostty",
	"rio", "terminal", "iterm", "hyper", "warp", "tabby", "windowsterminal", "cmd", "conhost",
	"powershell", "pwsh", "mintty", "putty", "consolewindowclass", "cascadia_hosting_window_class",
	"virtualconsoleclass", "console",
}

// IsTerminal reports whether name (an application name, process name or
// window class) is a terminal emulator or console.
func IsTerminal(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	for _, t := range terminals {
		if name == t {
			return true
		}
		if len(t) >= 5 && strings.Contains(name, t) {
			return true
		}
		if strings.HasPrefix(name, t) && len(name) > len(t) && strings.ContainsRune("-_. ", rune(name[len(t)])) {
			return true
		}
	}
	return false
}
