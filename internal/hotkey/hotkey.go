// Package hotkey registers the configurable global hotkey that triggers a
// capture.
//
// Unlike capture and ui, this one genuinely needs a native hook - "tell me
// when this key combination is pressed, anywhere, even when no window of
// mine has focus" has no shell-tool equivalent on any of the three
// platforms the way a copy-keystroke or a dialog box does. golang.design/x
// /hotkey (https://github.com/golang-design/hotkey) is used here because it
// covers exactly Windows, macOS and Linux/X11 with one API; its Linux
// backend is X11-only, same limitation noted in internal/capture, so on
// Wayland the hotkey itself may need to be bound through the compositor's
// own shortcut settings instead (see Register's doc).
package hotkey

import (
	"fmt"
	"strings"

	gohotkey "golang.design/x/hotkey"
)

// Parse turns a config string like "Ctrl+Alt+Space" or "CommandOrControl+
// Shift+N" into the modifiers+key golang.design/x/hotkey wants. Recognized
// modifier names: Ctrl/Control, Alt/Option, Shift, Cmd/Command/Super/Win -
// "CommandOrControl" is accepted as a synonym people will recognize from
// Electron's accelerator syntax and expands to Cmd on macOS, Ctrl
// elsewhere.
func Parse(spec string) ([]gohotkey.Modifier, gohotkey.Key, error) {
	parts := strings.Split(spec, "+")
	if len(parts) < 2 {
		return nil, 0, fmt.Errorf("%q is not a hotkey (need at least one modifier and a key, e.g. %q)", spec, "Ctrl+Alt+Space")
	}
	var mods []gohotkey.Modifier
	for _, p := range parts[:len(parts)-1] {
		mod, err := parseModifier(strings.TrimSpace(p))
		if err != nil {
			return nil, 0, err
		}
		mods = append(mods, mod)
	}
	key, err := parseKey(strings.TrimSpace(parts[len(parts)-1]))
	if err != nil {
		return nil, 0, err
	}
	return mods, key, nil
}

func parseModifier(name string) (gohotkey.Modifier, error) {
	switch strings.ToLower(name) {
	case "ctrl", "control":
		return gohotkey.ModCtrl, nil
	case "alt", "option":
		return modAlt(), nil
	case "shift":
		return gohotkey.ModShift, nil
	case "cmd", "command", "super", "win", "meta":
		return modSuper(), nil
	case "commandorcontrol":
		return modPrimary(), nil
	default:
		return 0, fmt.Errorf("unknown hotkey modifier %q", name)
	}
}

func parseKey(name string) (gohotkey.Key, error) {
	if k, ok := keyByName[strings.ToUpper(name)]; ok {
		return k, nil
	}
	return 0, fmt.Errorf("unknown hotkey key %q", name)
}

var keyByName = map[string]gohotkey.Key{
	"SPACE": gohotkey.KeySpace,
	"A":     gohotkey.KeyA, "B": gohotkey.KeyB, "C": gohotkey.KeyC, "D": gohotkey.KeyD,
	"E": gohotkey.KeyE, "F": gohotkey.KeyF, "G": gohotkey.KeyG, "H": gohotkey.KeyH,
	"I": gohotkey.KeyI, "J": gohotkey.KeyJ, "K": gohotkey.KeyK, "L": gohotkey.KeyL,
	"M": gohotkey.KeyM, "N": gohotkey.KeyN, "O": gohotkey.KeyO, "P": gohotkey.KeyP,
	"Q": gohotkey.KeyQ, "R": gohotkey.KeyR, "S": gohotkey.KeyS, "T": gohotkey.KeyT,
	"U": gohotkey.KeyU, "V": gohotkey.KeyV, "W": gohotkey.KeyW, "X": gohotkey.KeyX,
	"Y": gohotkey.KeyY, "Z": gohotkey.KeyZ,
	"0": gohotkey.Key0, "1": gohotkey.Key1, "2": gohotkey.Key2, "3": gohotkey.Key3,
	"4": gohotkey.Key4, "5": gohotkey.Key5, "6": gohotkey.Key6, "7": gohotkey.Key7,
	"8": gohotkey.Key8, "9": gohotkey.Key9,
	"RETURN": gohotkey.KeyReturn,
	"F1":     gohotkey.KeyF1, "F2": gohotkey.KeyF2, "F3": gohotkey.KeyF3, "F4": gohotkey.KeyF4,
	"F5": gohotkey.KeyF5, "F6": gohotkey.KeyF6, "F7": gohotkey.KeyF7, "F8": gohotkey.KeyF8,
	"F9": gohotkey.KeyF9, "F10": gohotkey.KeyF10, "F11": gohotkey.KeyF11, "F12": gohotkey.KeyF12,
}

// Handle is a registered hotkey; call Unregister when done with it (e.g. on
// a settings change that re-registers with a new combination).
type Handle struct {
	hk *gohotkey.Hotkey
}

// Register binds spec (see Parse) so that fn is called, in its own
// goroutine, each time it fires. On Wayland, golang.design/x/hotkey's X11
// backend will not see key presses at all; callers should detect that case
// (os.Getenv("WAYLAND_DISPLAY") != "") and tell the person to bind the
// hotkey in their desktop environment's own shortcut settings instead, to
// run `trecs-notes capture` (see cmd/trecs-notes) - a per-compositor global
// shortcut that shells out to this same binary, same end result, just
// configured outside the app rather than inside it.
func Register(spec string, fn func()) (*Handle, error) {
	mods, key, err := Parse(spec)
	if err != nil {
		return nil, err
	}
	hk := gohotkey.New(mods, key)
	if err := hk.Register(); err != nil {
		return nil, fmt.Errorf("registering hotkey %q: %w", spec, err)
	}
	go func() {
		for range hk.Keydown() {
			fn()
		}
	}()
	return &Handle{hk: hk}, nil
}

// Unregister releases the hotkey. CAUTION, Linux: golang.design/x/hotkey's
// Unregister waits for its X11 loop to notice the cancel, and that loop only
// wakes on an X event - so this call can block until the next hotkey press.
// Don't call it on the way out of the process (just exit; the X server drops
// the grab), only when re-registering after a settings change, and then from
// a goroutine.
func (h *Handle) Unregister() error {
	if h == nil || h.hk == nil {
		return nil
	}
	return h.hk.Unregister()
}
