//go:build linux

package capture

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// x11Native reads the focused window's identity straight from the X server,
// the same properties libwnck (and so devilspie2) reads - _NET_ACTIVE_WINDOW
// on the root window, then WM_CLASS and _NET_WM_NAME on that window. It is
// pure Go: no daemon, no helper tool, and it works under any EWMH-compliant
// window manager (XFCE, GNOME/X11, KDE/X11, i3, openbox...) and for
// XWayland clients on Wayland.
//
// The application name is WM_CLASS's class part ("firefox-esr", "XTerm");
// failing that its instance part, failing that the owning process's name
// (_NET_WM_PID -> /proc/<pid>/comm).
func x11Native() (app, title string, err error) {
	c, err := xgb.NewConn()
	if err != nil {
		return "", "", fmt.Errorf("cannot connect to the X server: %w", err)
	}
	defer c.Close()
	root := xproto.Setup(c).DefaultScreen(c).Root

	atom := func(name string) xproto.Atom {
		r, err := xproto.InternAtom(c, false, uint16(len(name)), name).Reply()
		if err != nil {
			return 0
		}
		return r.Atom
	}
	prop := func(w xproto.Window, a, typ xproto.Atom, words uint32) []byte {
		if a == 0 {
			return nil
		}
		r, err := xproto.GetProperty(c, false, w, a, typ, 0, words).Reply()
		if err != nil || r.ValueLen == 0 {
			return nil
		}
		return r.Value
	}

	v := prop(root, atom("_NET_ACTIVE_WINDOW"), xproto.AtomWindow, 1)
	if len(v) < 4 {
		return "", "", fmt.Errorf("the window manager does not publish _NET_ACTIVE_WINDOW")
	}
	win := xproto.Window(xgb.Get32(v))
	if win == 0 {
		return "", "", fmt.Errorf("no window has focus")
	}

	// WM_CLASS: "instance\0class\0"
	if v := prop(win, xproto.AtomWmClass, xproto.AtomString, 256); v != nil {
		parts := strings.Split(strings.TrimRight(string(v), "\x00"), "\x00")
		switch {
		case len(parts) > 1 && parts[1] != "":
			app = parts[1]
		case parts[0] != "":
			app = parts[0]
		}
	}
	if app == "" {
		if v := prop(win, atom("_NET_WM_PID"), xproto.AtomCardinal, 1); len(v) >= 4 {
			if b, err := os.ReadFile("/proc/" + strconv.Itoa(int(xgb.Get32(v))) + "/comm"); err == nil {
				app = strings.TrimSpace(string(b))
			}
		}
	}

	if v := prop(win, atom("_NET_WM_NAME"), atom("UTF8_STRING"), 1024); v != nil {
		title = string(v)
	} else if v := prop(win, xproto.AtomWmName, xproto.AtomString, 1024); v != nil {
		title = string(v)
	}
	return app, title, nil
}
