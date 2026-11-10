//go:build linux

package gui

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/jezek/xgb"
)

// Available says whether there is a graphical session to open windows and a
// tray icon in: an X server or a Wayland compositor that answers. A DISPLAY
// that is set but dead (a terminal multiplexer that outlived the ssh
// connection that forwarded it) counts as none.
func Available() error {
	x, w := os.Getenv("DISPLAY"), os.Getenv("WAYLAND_DISPLAY")
	if x == "" && w == "" {
		return errors.New("neither DISPLAY nor WAYLAND_DISPLAY is set")
	}
	var why []string
	if x != "" {
		ok := make(chan error, 1)
		go func() {
			c, err := xgb.NewConn()
			if err == nil {
				c.Close()
			}
			ok <- err
		}()
		select {
		case err := <-ok:
			if err == nil {
				return nil
			}
			why = append(why, fmt.Sprintf("X server %q does not answer", x))
		case <-time.After(3 * time.Second):
			why = append(why, fmt.Sprintf("X server %q does not answer in time", x))
		}
	}
	if w != "" {
		path := w
		if !filepath.IsAbs(path) {
			path = filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), w)
		}
		c, err := net.DialTimeout("unix", path, 2*time.Second)
		if err == nil {
			_ = c.Close()
			return nil
		}
		why = append(why, fmt.Sprintf("Wayland socket %q does not answer", path))
	}
	return errors.New(why[0] + moreWhy(why))
}

func moreWhy(why []string) string {
	if len(why) > 1 {
		return "; " + why[1]
	}
	return ""
}
