//go:build darwin || windows

package hotkey

import (
	"fmt"

	gohotkey "golang.design/x/hotkey"
)

func register(s Spec, fn func()) (*Handle, error) {
	var mods []gohotkey.Modifier
	if s.Ctrl {
		mods = append(mods, gohotkey.ModCtrl)
	}
	if s.Alt {
		mods = append(mods, modAlt())
	}
	if s.Shift {
		mods = append(mods, gohotkey.ModShift)
	}
	if s.Super {
		mods = append(mods, modSuper())
	}
	key, ok := keyByName[s.Key]
	if !ok {
		return nil, fmt.Errorf("unknown key %q", s.Key)
	}
	hk := gohotkey.New(mods, key)
	if err := hk.Register(); err != nil {
		return nil, err
	}
	go func() {
		for range hk.Keydown() {
			go fn()
		}
	}()
	return &Handle{stop: func() { _ = hk.Unregister() }}, nil
}

var keyByName = map[string]gohotkey.Key{
	"SPACE": gohotkey.KeySpace, "RETURN": gohotkey.KeyReturn,
	"A": gohotkey.KeyA, "B": gohotkey.KeyB, "C": gohotkey.KeyC, "D": gohotkey.KeyD,
	"E": gohotkey.KeyE, "F": gohotkey.KeyF, "G": gohotkey.KeyG, "H": gohotkey.KeyH,
	"I": gohotkey.KeyI, "J": gohotkey.KeyJ, "K": gohotkey.KeyK, "L": gohotkey.KeyL,
	"M": gohotkey.KeyM, "N": gohotkey.KeyN, "O": gohotkey.KeyO, "P": gohotkey.KeyP,
	"Q": gohotkey.KeyQ, "R": gohotkey.KeyR, "S": gohotkey.KeyS, "T": gohotkey.KeyT,
	"U": gohotkey.KeyU, "V": gohotkey.KeyV, "W": gohotkey.KeyW, "X": gohotkey.KeyX,
	"Y": gohotkey.KeyY, "Z": gohotkey.KeyZ,
	"0": gohotkey.Key0, "1": gohotkey.Key1, "2": gohotkey.Key2, "3": gohotkey.Key3,
	"4": gohotkey.Key4, "5": gohotkey.Key5, "6": gohotkey.Key6, "7": gohotkey.Key7,
	"8": gohotkey.Key8, "9": gohotkey.Key9,
	"F1": gohotkey.KeyF1, "F2": gohotkey.KeyF2, "F3": gohotkey.KeyF3, "F4": gohotkey.KeyF4,
	"F5": gohotkey.KeyF5, "F6": gohotkey.KeyF6, "F7": gohotkey.KeyF7, "F8": gohotkey.KeyF8,
	"F9": gohotkey.KeyF9, "F10": gohotkey.KeyF10, "F11": gohotkey.KeyF11, "F12": gohotkey.KeyF12,
}
