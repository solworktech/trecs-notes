// Command trecs-notes is a small cross-platform tray app: a global hotkey
// captures whatever text is selected, in whatever app has focus, and saves
// it as a note - always locally, and optionally also to a trecs server.
//
// Subcommands beyond the tray itself:
//
//	trecs-notes            run the tray app (the normal way to use it)
//	trecs-notes diagnose    print what this session offers (tools found,
//	                        session type) and what a capture reads right now
//	trecs-notes capture     do one capture-and-save without a tray icon -
//	                        meant to be bound as a shortcut inside a Wayland
//	                        compositor's own settings, where this app's own
//	                        global-hotkey registration cannot work (see
//	                        internal/hotkey's package doc)
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/solworktech/trecs-notes/internal/capture"
	"github.com/solworktech/trecs-notes/internal/config"
	"github.com/solworktech/trecs-notes/internal/hotkey"
	"github.com/solworktech/trecs-notes/internal/localdb"
	"github.com/solworktech/trecs-notes/internal/model"
	"github.com/solworktech/trecs-notes/internal/remote"
	"github.com/solworktech/trecs-notes/internal/tray"
	"github.com/solworktech/trecs-notes/internal/ui"
)

// defaultServer is the server used when nothing else says otherwise; set at
// release time like in trecs:
//
//	-ldflags "-X main.defaultServer=https://trecs.run"
var defaultServer string

func main() {
	if defaultServer != "" {
		config.DefaultServer = defaultServer
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "capture":
			runOneCapture(false)
			return
		case "quick": // like the quick-save hotkey: store the selection, no form
			runOneCapture(true)
			return
		case "dialog": // internal: the capture form, run as a child process (see internal/ui)
			ui.SetIcon(trayIconPNG)
			os.Exit(ui.RunDialogProcess())
		case "settings": // internal: the Settings window, run as a child process
			ui.SetIcon(trayIconPNG)
			os.Exit(runSettingsProcess())
		case "hotkey-check": // internal: can this combination be grabbed? exit 0 = yes
			os.Exit(runHotkeyCheck(os.Args[2:]))
		case "diagnose":
			runDiagnose()
			return
		}
	}
	runTray()
}

// app bundles the state a capture needs: the DB, the current config (for
// whether to dual-write and where), and a context for the whole process's
// lifetime.
type app struct {
	db  *localdb.DB
	cfg config.Config
}

func newApp() (*app, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	dir, err := config.NotesDir()
	if err != nil {
		return nil, err
	}
	db, err := localdb.Open(dir + "/notes.db")
	if err != nil {
		return nil, fmt.Errorf("opening local database: %w", err)
	}
	return &app{db: db, cfg: cfg}, nil
}

func runOneCapture(quick bool) {
	a, err := newApp()
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = a.db.Close() }()
	if quick {
		a.doQuickSave()
		return
	}
	a.doCapture()
}

func runTray() {
	a, err := newApp()
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = a.db.Close() }()

	ui.SetIcon(trayIconPNG)

	// quit ends the process. It deliberately does NOT call hk.Unregister():
	// on Linux that blocks until the next hotkey is pressed (it waits for a
	// C call that only returns on an X event), which hung Quit. The X server
	// drops our key grab by itself when the process exits.
	quit := func() {
		ui.CloseDialog() // an open capture form must not outlive the app
		_ = a.db.Close()
		os.Exit(0)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() { <-sig; quit() }()

	_, err = hotkey.Register(a.cfg.Hotkey, a.doCapture)
	if err != nil {
		// Not fatal: on Wayland this is expected (see internal/hotkey's
		// doc) - the tray still runs, Browse/Settings still work, the
		// person just needs to bind `trecs-notes capture` in their
		// compositor's own shortcut settings instead.
		log.Printf("hotkey not registered (%v); bind `trecs-notes capture` in your desktop's own shortcut settings instead", err)
	}

	a.registerQuick()

	tray.Run(trayIconPNG, tray.Callbacks{
		OnSettings: a.openSettings,
		OnBrowse:   a.openBrowse,
		OnQuit:     quit,
	})
}

// registerQuick binds the quick-save hotkey, if there is one and it is not
// the same as the form's.
func (a *app) registerQuick() {
	spec := a.cfg.QuickHotkey
	if spec == "" || spec == a.cfg.Hotkey {
		return
	}
	if _, err := hotkey.Register(spec, a.doQuickSave); err != nil {
		log.Printf("quick-save hotkey not registered (%v); bind `trecs-notes quick` in your desktop's own shortcut settings instead", err)
	}
}

// doQuickSave is the quick-save hotkey's whole job: capture and store, with
// no form. Without selected text it does nothing.
func (a *app) doQuickSave() {
	if cfg, err := config.Load(); err == nil {
		a.cfg = cfg
	}
	snap := capture.Capture(capture.Options{CopyFallback: a.cfg.CopyFallback})
	for _, w := range snap.Warnings {
		log.Printf("capture: %s", w)
	}
	note, ok := snap.QuickNote()
	if !ok {
		log.Printf("quick save: no selected text, nothing stored")
		return
	}
	a.save(note)
	log.Printf("quick save: stored %d characters from %q", len(note.Text), note.AppName)
}

// save stores a note and, if sync is on, sends it on in the background.
func (a *app) save(note model.Note) {
	if err := a.db.Insert(&note); err != nil {
		log.Printf("saving note: %v", err)
		return
	}
	if a.cfg.RemoteSyncEnabled {
		go func() {
			if err := remote.SyncNote(a.db, note); err != nil {
				log.Printf("sync: %v", err)
			}
		}()
	}
}

// doCapture is the hotkey's (and `trecs-notes capture`'s) entire job:
// capture -> show the dialog -> save.
func (a *app) doCapture() {
	// Settings may have changed since startup (size, remote sync).
	if cfg, err := config.Load(); err == nil {
		a.cfg = cfg
	}
	snap := capture.Capture(capture.Options{CopyFallback: a.cfg.CopyFallback})
	for _, w := range snap.Warnings {
		log.Printf("capture: %s", w)
	}

	suggestions, err := a.db.TagSuggestions("", 10)
	if err != nil {
		log.Printf("tag suggestions: %v", err)
	}

	result, err := ui.ShowCaptureDialog(ui.Input{Snapshot: snap, TagSuggestions: suggestions, Width: a.cfg.Width, Height: a.cfg.Height})
	if err != nil {
		log.Printf("capture dialog: %v", err)
		return
	}
	if !result.Confirmed {
		return
	}

	tags, err := model.NormalizeTags(model.SplitTagInput(result.TagsRaw))
	if err != nil {
		log.Printf("tags: %v", err)
		// Saved without the offending tags rather than losing the note
		// entirely over a tag typo; the person can fix tags later from
		// Browse notes.
		tags = nil
	}

	note := model.Note{
		AppName:     result.AppName,
		WindowTitle: result.WindowTitle,
		Text:        result.Text,
		Tags:        tags,
	}
	a.save(note)
}

// openBrowse is a placeholder for the next pass: list/search notes.
func (a *app) openBrowse() { log.Println("Browse notes: not yet implemented") }

// runDiagnose answers "why is the application / window title blank?": it
// shows the session and which helper tools exist, then, after a pause in
// which to focus a window with some text selected, what a capture returns.
func runDiagnose() {
	fmt.Printf("GOOS=%s XDG_SESSION_TYPE=%q XDG_CURRENT_DESKTOP=%q WAYLAND_DISPLAY=%q DISPLAY=%q\n",
		runtime.GOOS, os.Getenv("XDG_SESSION_TYPE"), os.Getenv("XDG_CURRENT_DESKTOP"),
		os.Getenv("WAYLAND_DISPLAY"), os.Getenv("DISPLAY"))
	for _, t := range []string{"xdotool", "xclip", "xsel", "wl-paste", "wtype", "swaymsg", "hyprctl", "niri", "kdotool", "gdbus", "yad", "zenity"} {
		if p, err := exec.LookPath(t); err == nil {
			fmt.Printf("  %-9s %s\n", t, p)
		} else {
			fmt.Printf("  %-9s -\n", t)
		}
	}
	fmt.Println("Focus a window with some text selected; capturing in 4 seconds...")
	time.Sleep(4 * time.Second)
	cfg, _ := config.Load()
	snap := capture.Capture(capture.Options{CopyFallback: cfg.CopyFallback})
	fmt.Printf("app:    %q\ntitle:  %q\ntext:   %q\n", snap.AppName, snap.WindowTitle, snap.SelectedText)
	for _, w := range snap.Warnings {
		fmt.Println("warning:", w)
	}
}
