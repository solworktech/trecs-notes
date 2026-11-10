//go:build linux

package hotkey

import (
	"errors"
	"fmt"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

const (
	maskLock = xproto.ModMaskLock // Caps Lock
	maskNum  = xproto.ModMask2    // Num Lock on a standard keymap
)

// keysym is the X keysym of a key name from Parse.
func keysym(key string) xproto.Keysym {
	switch {
	case key == "SPACE":
		return 0x20
	case key == "RETURN":
		return 0xff0d
	case key[0] == 'F' && len(key) > 1:
		var n int
		_, _ = fmt.Sscanf(key[1:], "%d", &n)
		return xproto.Keysym(0xffbe + n - 1)
	case key[0] >= 'A' && key[0] <= 'Z':
		return xproto.Keysym(key[0] - 'A' + 'a')
	}
	return xproto.Keysym(key[0]) // digits
}

// grabber holds one hotkey's grab on one connection.
type grabber struct {
	conn *xgb.Conn
	root xproto.Window
	mods uint16
	sym  xproto.Keysym
	code xproto.Keycode
}

// grab finds the key that has the keysym now and grabs it with each state of
// Caps Lock and Num Lock (they count as modifiers in an event).
func (g *grabber) grab() error {
	setup := xproto.Setup(g.conn)
	first, last := setup.MinKeycode, setup.MaxKeycode
	km, err := xproto.GetKeyboardMapping(g.conn, first, byte(last-first+1)).Reply()
	if err != nil {
		return err
	}
	var code xproto.Keycode
	per := int(km.KeysymsPerKeycode)
	for kc := int(first); kc <= int(last) && code == 0; kc++ {
		for i := 0; i < per && i < 2; i++ { // unshifted and shifted columns
			if km.Keysyms[(kc-int(first))*per+i] == g.sym {
				code = xproto.Keycode(kc)
				break
			}
		}
	}
	if code == 0 {
		return errors.New("the keyboard has no such key")
	}
	if g.code != 0 && g.code != code {
		_ = xproto.UngrabKeyChecked(g.conn, g.code, g.root, xproto.ModMaskAny).Check()
	}
	for _, extra := range []uint16{0, maskLock, maskNum, maskLock | maskNum} {
		err := xproto.GrabKeyChecked(g.conn, true, g.root, g.mods|extra, code,
			xproto.GrabModeAsync, xproto.GrabModeAsync).Check()
		if err != nil {
			return err // BadAccess: another program has it
		}
	}
	g.code = code
	return nil
}

func register(s Spec, fn func()) (*Handle, error) {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("no X display to grab keys on (%w)", err)
	}
	g := &grabber{conn: conn, root: xproto.Setup(conn).DefaultScreen(conn).Root, sym: keysym(s.Key)}
	if s.Ctrl {
		g.mods |= xproto.ModMaskControl
	}
	if s.Alt {
		g.mods |= xproto.ModMask1
	}
	if s.Shift {
		g.mods |= xproto.ModMaskShift
	}
	if s.Super {
		g.mods |= xproto.ModMask4
	}
	if err := g.grab(); err != nil {
		conn.Close()
		return nil, err
	}

	go func() {
		for {
			ev, err := conn.WaitForEvent()
			if ev == nil && err == nil {
				return // closed
			}
			switch e := ev.(type) {
			case xproto.KeyPressEvent:
				if e.Detail == g.code && e.State&0xff&^(maskLock|maskNum) == g.mods {
					go fn()
				}
			case xproto.MappingNotifyEvent:
				// the keyboard layout changed: the key may be elsewhere now
				if e.Request == xproto.MappingKeyboard {
					_ = g.grab()
				}
			}
		}
	}()
	return &Handle{stop: conn.Close}, nil
}
