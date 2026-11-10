// Package hotkey registers the configurable global hotkeys.
//
// Unlike capture and ui, this one genuinely needs a native hook - "tell me
// when this key combination is pressed, anywhere, even when no window of
// mine has focus" has no shell-tool equivalent. On Windows and macOS
// golang.design/x/hotkey does it. On Linux it is done here, over X11 in pure
// Go (github.com/jezek/xgb), and not with that library, for a reason that has
// nothing to do with hotkeys: golang.design/x/hotkey panics in its package
// initialisation when there is no X display, which would make the whole
// binary - the terminal interface included - refuse to start on a machine
// with no display. It also brings in libX11 through cgo. This version just
// returns an error when there is no display.
//
// X11 only: on Wayland a program cannot grab keys, so the combination has to
// be bound in the compositor's or desktop's own shortcut settings, to run
// `trecs-notes capture` (or `trecs-notes quick`).
package hotkey

import (
	"fmt"
	"runtime"
	"strings"
)

// Spec is a parsed hotkey, free of any platform's constants.
type Spec struct {
	Ctrl, Alt, Shift, Super bool
	Key                     string // upper case: "A", "5", "SPACE", "RETURN", "F7"
}

// Parse turns a config string like "Ctrl+Alt+Space" or "CommandOrControl+
// Shift+N" into a Spec. Recognized modifier names: Ctrl/Control, Alt/Option,
// Shift, Cmd/Command/Super/Win/Meta - "CommandOrControl" is accepted as a
// synonym people will recognize from Electron's accelerator syntax and means
// Cmd on macOS, Ctrl elsewhere. At least one modifier is needed.
func Parse(spec string) (Spec, error) {
	parts := strings.Split(spec, "+")
	if len(parts) < 2 {
		return Spec{}, fmt.Errorf("%q is not a hotkey (need at least one modifier and a key, e.g. %q)", spec, "Ctrl+Alt+Space")
	}
	var s Spec
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "ctrl", "control":
			s.Ctrl = true
		case "alt", "option":
			s.Alt = true
		case "shift":
			s.Shift = true
		case "cmd", "command", "super", "win", "meta":
			s.Super = true
		case "commandorcontrol":
			if runtime.GOOS == "darwin" {
				s.Super = true
			} else {
				s.Ctrl = true
			}
		default:
			return Spec{}, fmt.Errorf("unknown hotkey modifier %q", strings.TrimSpace(p))
		}
	}
	key := strings.ToUpper(strings.TrimSpace(parts[len(parts)-1]))
	if !validKey(key) {
		return Spec{}, fmt.Errorf("unknown hotkey key %q", strings.TrimSpace(parts[len(parts)-1]))
	}
	s.Key = key
	return s, nil
}

func validKey(k string) bool {
	switch {
	case k == "SPACE" || k == "RETURN":
		return true
	case len(k) == 1 && (k[0] >= 'A' && k[0] <= 'Z' || k[0] >= '0' && k[0] <= '9'):
		return true
	case len(k) >= 2 && len(k) <= 3 && k[0] == 'F':
		var n int
		if _, err := fmt.Sscanf(k[1:], "%d", &n); err == nil && n >= 1 && n <= 12 && fmt.Sprint(n) == k[1:] {
			return true
		}
	}
	return false
}

// Handle is a registered hotkey; call Unregister when done with it.
type Handle struct {
	stop func()
}

// Register binds spec (see Parse) so that fn is called, in its own
// goroutine, each time it fires. It fails when the combination cannot be
// grabbed (another program holds it) or, on Linux, when there is no X display.
func Register(spec string, fn func()) (*Handle, error) {
	s, err := Parse(spec)
	if err != nil {
		return nil, err
	}
	h, err := register(s, fn)
	if err != nil {
		return nil, fmt.Errorf("registering hotkey %q: %w", spec, err)
	}
	return h, nil
}

// Unregister releases the hotkey.
func (h *Handle) Unregister() error {
	if h != nil && h.stop != nil {
		h.stop()
	}
	return nil
}
