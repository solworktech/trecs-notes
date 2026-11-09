//go:build linux

package capture

import (
	"os"
	"testing"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// Needs an X server (run under xvfb-run); skipped otherwise. It creates a
// window, gives it WM_CLASS and _NET_WM_NAME, publishes it as the active
// window, and checks x11Native reads all of it back.
func TestX11Native(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("no X display")
	}
	c, err := xgb.NewConn()
	if err != nil {
		t.Skip(err)
	}
	defer c.Close()
	scr := xproto.Setup(c).DefaultScreen(c)
	win, _ := xproto.NewWindowId(c)
	xproto.CreateWindow(c, scr.RootDepth, win, scr.Root, 0, 0, 10, 10, 0, xproto.WindowClassInputOutput, scr.RootVisual, 0, nil)
	atom := func(n string) xproto.Atom {
		r, _ := xproto.InternAtom(c, false, uint16(len(n)), n).Reply()
		return r.Atom
	}
	xproto.ChangeProperty(c, xproto.PropModeReplace, win, xproto.AtomWmClass, xproto.AtomString, 8, 22, []byte("Navigator\x00firefox-esr\x00"))
	title := "Trecs - Use case: DevOps — Mozilla Firefox"
	xproto.ChangeProperty(c, xproto.PropModeReplace, win, atom("_NET_WM_NAME"), atom("UTF8_STRING"), 8, uint32(len(title)), []byte(title))
	id := []byte{byte(win), byte(win >> 8), byte(win >> 16), byte(win >> 24)}
	xproto.ChangeProperty(c, xproto.PropModeReplace, scr.Root, atom("_NET_ACTIVE_WINDOW"), xproto.AtomWindow, 32, 1, id)
	c.Sync()

	app, got, err := x11Native()
	if err != nil || app != "firefox-esr" || got != title {
		t.Fatalf("app=%q title=%q err=%v", app, got, err)
	}
}
