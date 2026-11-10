// Package gui is the graphical frontend: the tray icon, the global hotkeys,
// and the windows (the capture form, Settings) they open. It holds no logic
// of its own about notes or settings - that is internal/notes, shared with
// the terminal frontend (internal/tui) - only what is about screens, keys and
// processes.
package gui

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/solworktech/trecs-notes/internal/capture"
	"github.com/solworktech/trecs-notes/internal/config"
	"github.com/solworktech/trecs-notes/internal/hotkey"
	"github.com/solworktech/trecs-notes/internal/model"
	"github.com/solworktech/trecs-notes/internal/notes"
	"github.com/solworktech/trecs-notes/internal/tray"
	"github.com/solworktech/trecs-notes/internal/ui"
)

// icon is the tray and window icon (PNG), set by the command, which embeds it.
var icon []byte

// SetIcon sets the icon used by the tray and by every window.
func SetIcon(png []byte) {
	icon = png
	ui.SetIcon(png)
}

// RunDialog is `trecs-notes dialog` (internal): the capture form, in a child
// process of the tray, so GTK stays out of it.
func RunDialog() int { return ui.RunDialogProcess() }

// RunOneCapture is `trecs-notes capture` and `trecs-notes quick`: one capture
// without a tray icon, meant to be bound to a shortcut in a Wayland
// compositor's own settings, where the app's own hotkeys cannot work.
func RunOneCapture(quick bool) {
	s, err := notes.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	a := &app{Service: s}
	if quick {
		a.doQuickSave()
		return
	}
	a.doCapture()
}

type app struct {
	*notes.Service

	handles []*hotkey.Handle
	bound   config.Config // the hotkeys grabbed (only those two fields are set)
	restart sync.Once
}

// RunTray runs the tray app: the hotkeys, and the menu. It returns only when
// the process is ending.
func RunTray() {
	s, err := notes.Open()
	if err != nil {
		log.Fatal(err)
	}
	a := &app{Service: s}
	defer func() { _ = s.Close() }()

	// quit ends the process.
	quit := func() {
		ui.CloseDialog() // an open capture form must not outlive the app
		_ = s.Close()
		os.Exit(0)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() { <-sig; quit() }()

	a.bound = config.Config{Hotkey: a.Cfg.Hotkey, QuickHotkey: a.Cfg.QuickHotkey}
	if h, err := hotkey.Register(a.Cfg.Hotkey, a.doCapture); err == nil {
		a.handles = append(a.handles, h)
	} else {
		// Not fatal: on Wayland this is expected (see internal/hotkey's
		// doc) - the tray still runs, Browse/Settings still work, the
		// person just needs to bind `trecs-notes capture` in their
		// compositor's own shortcut settings instead.
		log.Printf("hotkey not registered (%v); bind `trecs-notes capture` in your desktop's own shortcut settings instead", err)
	}
	a.registerQuick()
	go a.watchConfig()

	tray.Run(icon, tray.Callbacks{
		OnSettings: a.openSettings,
		OnBrowse:   a.openBrowse,
		OnQuit:     quit,
	})
}

// watchConfig notices when the config file changes - from the Settings
// window, the terminal interface, an editor - and restarts the app if its
// hotkeys did: they are grabbed when it starts.
func (a *app) watchConfig() {
	path, err := config.Path()
	if err != nil {
		return
	}
	stamp := func() string {
		fi, err := os.Stat(path)
		if err != nil {
			return ""
		}
		return fi.ModTime().String() + "/" + strconv.FormatInt(fi.Size(), 10)
	}
	last := stamp()
	for range time.Tick(time.Second) {
		if now := stamp(); now == last {
			continue
		} else {
			last = now
		}
		cfg, err := config.Load()
		if err != nil {
			continue
		}
		if notes.HotkeysChanged(a.bound, cfg) {
			a.restartApp()
			return
		}
	}
}

// restartApp starts a fresh copy, which grabs the new hotkeys, and ends this
// one. The grabs are let go first, so that a key that did not change is free
// for the new copy to take.
func (a *app) restartApp() {
	a.restart.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			log.Printf("restart: %v", err)
			return
		}
		log.Printf("hotkeys changed: restarting")
		for _, h := range a.handles {
			_ = h.Unregister()
		}
		next := exec.Command(exe)
		next.Stdout, next.Stderr = os.Stdout, os.Stderr
		if err := next.Start(); err != nil {
			log.Printf("restart: %v (start trecs-notes again to use the new hotkeys)", err)
			return
		}
		ui.CloseDialog()
		_ = a.Close()
		os.Exit(0)
	})
}

// registerQuick binds the quick-save hotkey, if there is one and it is not
// the same as the form's.
func (a *app) registerQuick() {
	spec := a.Cfg.QuickHotkey
	if spec == "" || spec == a.Cfg.Hotkey {
		return
	}
	if h, err := hotkey.Register(spec, a.doQuickSave); err == nil {
		a.handles = append(a.handles, h)
	} else {
		log.Printf("quick-save hotkey not registered (%v); bind `trecs-notes quick` in your desktop's own shortcut settings instead", err)
	}
}

// doQuickSave is the quick-save hotkey's whole job: capture and store, with
// no form. Without selected text it does nothing.
func (a *app) doQuickSave() {
	a.Reload()
	snap := capture.Capture(capture.Options{CopyFallback: a.Cfg.CopyFallback})
	for _, w := range snap.Warnings {
		log.Printf("capture: %s", w)
	}
	stored, err := a.QuickSave(snap)
	switch {
	case err != nil:
		log.Printf("quick save: %v", err)
	case !stored:
		log.Printf("quick save: no selected text, nothing stored")
	default:
		log.Printf("quick save: stored %d characters from %q", len(snap.SelectedText), snap.AppName)
	}
}

// doCapture is the hotkey's (and `trecs-notes capture`'s) entire job:
// capture -> show the dialog -> save.
func (a *app) doCapture() {
	a.Reload() // Settings may have changed since startup (size, remote sync)
	snap := capture.Capture(capture.Options{CopyFallback: a.Cfg.CopyFallback})
	for _, w := range snap.Warnings {
		log.Printf("capture: %s", w)
	}

	suggestions, err := a.DB.TagSuggestions("", 10)
	if err != nil {
		log.Printf("tag suggestions: %v", err)
	}

	result, err := ui.ShowCaptureDialog(ui.Input{Snapshot: snap, TagSuggestions: suggestions, Width: a.Cfg.Width, Height: a.Cfg.Height})
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
		// entirely over a tag typo; the person can fix tags later.
		tags = nil
	}
	if err := a.Save(model.Note{
		AppName:     result.AppName,
		WindowTitle: result.WindowTitle,
		Text:        result.Text,
		Tags:        tags,
	}); err != nil {
		log.Printf("saving note: %v", err)
	}
}

// Diagnose is `trecs-notes diagnose`: it answers "why is the application /
// window title blank?": it shows the session and which helper tools exist,
// then, after a pause in which to focus a window with some text selected,
// what a capture returns.
func Diagnose() {
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
	s, err := notes.Open()
	copyFallback := false
	if err == nil {
		copyFallback = s.Cfg.CopyFallback
		_ = s.Close()
	}
	snap := capture.Capture(capture.Options{CopyFallback: copyFallback})
	fmt.Printf("app:    %q\ntitle:  %q\ntext:   %q\n", snap.AppName, snap.WindowTitle, snap.SelectedText)
	for _, w := range snap.Warnings {
		fmt.Println("warning:", w)
	}
}
