// Command trecs-notes keeps notes: text selected in whatever application has
// focus, saved with the application's name and window title, always locally
// and optionally also to a trecs server.
//
// It has two frontends over the same code (internal/notes):
//
//   - the graphical one: a tray icon, global hotkeys and GTK windows
//     (internal/gui);
//   - the terminal one, built on tview (internal/tui).
//
// With no arguments it opens the graphical one if there is a graphical
// session, and the terminal one if there is none (or the graphical one cannot
// start). -tui opens the terminal one in any case.
//
//	trecs-notes            the graphical app, or the terminal one without a session
//	trecs-notes -tui       the terminal app
//	trecs-notes diagnose   print what this session offers (tools found,
//	                       session type) and what a capture reads right now
//	trecs-notes capture    one capture-and-save, with the form, without a tray
//	                       icon - meant to be bound as a shortcut inside a Wayland
//	                       compositor's own settings, where the app's own
//	                       hotkeys cannot work (see internal/hotkey)
//	trecs-notes quick      the same, but storing the selection at once, with no
//	                       form and no tags
//	trecs-notes browse     the window for browsing, searching and editing notes
//	                       (the tray menu's "Browse notes")
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/solworktech/trecs-notes/internal/config"
	"github.com/solworktech/trecs-notes/internal/gui"
	"github.com/solworktech/trecs-notes/internal/notes"
	"github.com/solworktech/trecs-notes/internal/tui"
)

// defaultServer is the server used when nothing else says otherwise; set at
// release time like in trecs:
//
//	-ldflags "-X main.defaultServer=https://trecs.run"
var defaultServer string

const usage = `usage: trecs-notes [-tui]
       trecs-notes capture | quick | browse | diagnose
`

func main() {
	if defaultServer != "" {
		config.DefaultServer = defaultServer
	}
	gui.SetIcon(trayIconPNG)

	args := os.Args[1:]
	if len(args) == 0 {
		if err := gui.Available(); err != nil {
			fmt.Fprintf(os.Stderr, "trecs-notes: no graphical session (%v); starting the terminal interface\n", err)
			runTUI()
			return
		}
		gui.RunTray()
		return
	}
	switch args[0] {
	case "-tui", "--tui":
		runTUI()
	case "capture":
		gui.RunOneCapture(false)
	case "quick": // like the quick-save hotkey: store the selection, no form
		gui.RunOneCapture(true)
	case "dialog": // internal: the capture form, run as a child process (see internal/ui)
		os.Exit(gui.RunDialog())
	case "settings": // internal: the Settings window, run as a child process
		os.Exit(gui.RunSettings())
	case "browse": // internal: the notes window, run as a child process
		os.Exit(gui.RunBrowse())
	case "hotkey-check": // internal: can this combination be grabbed? exit 0 = yes
		os.Exit(gui.HotkeyCheck(args[1:]))
	case "diagnose":
		gui.Diagnose()
	case "-h", "-help", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "trecs-notes: unknown argument %q\n%s", args[0], usage)
		os.Exit(2)
	}
}

func runTUI() {
	s, err := notes.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "trecs-notes:", err)
		os.Exit(1)
	}
	err = tui.Run(s)
	s.Flush(5 * time.Second) // a change made just before quitting still goes
	_ = s.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "trecs-notes:", err)
		os.Exit(1)
	}
}
