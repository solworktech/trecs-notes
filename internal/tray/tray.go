// Package tray is the system tray icon and its menu: Settings, Browse notes,
// Quit, per the spec - more items can be added later without changing this
// package's shape.
//
// Built on fyne.io/systray (the maintained fork of the original
// getlantern/systray), which covers Windows, macOS and Linux. On Linux it
// talks to whatever implements the StatusNotifierItem/AppIndicator
// protocol (GNOME needs the AppIndicator extension installed; KDE, XFCE
// and most others support it out of the box) - a tray icon that silently
// doesn't appear on a bare GNOME session is a known rough edge of that
// protocol, not a bug in this package.
package tray

import (
	"fyne.io/systray"
)

// Callbacks are the three menu actions' handlers, run on their own
// goroutine so a slow one (e.g. opening a browser for Settings) never
// blocks the tray's event loop.
type Callbacks struct {
	OnSettings func()
	OnBrowse   func()
	OnQuit     func()
}

// Run blocks until Quit is chosen (or the process is otherwise asked to
// exit); iconPNG is the tray icon's image data, embedded by the caller
// (see cmd/trecs-notes) rather than read from disk, so the binary stays a
// single file to ship.
func Run(iconPNG []byte, cb Callbacks) {
	systray.Run(func() { onReady(iconPNG, cb) }, func() {})
}

func onReady(iconPNG []byte, cb Callbacks) {
	systray.SetIcon(iconPNG)
	systray.SetTooltip("Trecs Notes")

	mSettings := systray.AddMenuItem("Settings", "Configure the hotkey and remote sync")
	mBrowse := systray.AddMenuItem("Browse notes", "Search and view saved notes")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Quit Trecs Notes")

	go func() {
		for {
			select {
			case <-mSettings.ClickedCh:
				if cb.OnSettings != nil {
					go cb.OnSettings()
				}
			case <-mBrowse.ClickedCh:
				if cb.OnBrowse != nil {
					go cb.OnBrowse()
				}
			case <-mQuit.ClickedCh:
				if cb.OnQuit != nil {
					cb.OnQuit()
				}
				systray.Quit()
				return
			}
		}
	}()
}
