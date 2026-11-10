package gui

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/solworktech/trecs-notes/internal/config"
	"github.com/solworktech/trecs-notes/internal/hotkey"
	"github.com/solworktech/trecs-notes/internal/localdb"
	"github.com/solworktech/trecs-notes/internal/notes"
	"github.com/solworktech/trecs-notes/internal/ui"
)

var settingsOpen sync.Mutex

// openSettings runs the Settings window as a child process (GTK stays out of
// the tray process) and applies what it saved: the config file is the
// hand-off.
func (a *app) openSettings() {
	if !settingsOpen.TryLock() {
		return // already open
	}
	defer settingsOpen.Unlock()

	exe, err := os.Executable()
	if err != nil {
		log.Printf("settings: %v", err)
		return
	}
	cmd := exec.Command(exe, "settings")
	if n, err := a.DB.CountUnsynced(); err == nil {
		cmd.Env = append(os.Environ(), "TRECS_UNSYNCED="+strconv.Itoa(n))
	}
	cmd.Stderr = os.Stderr
	if err := ui.RunChild(cmd); err != nil {
		if exitCodeOf(err) == 2 {
			// No settings window on this system: the config file is the
			// fallback, and it is plain JSON.
			if dir, derr := config.NotesDir(); derr == nil {
				log.Printf("no settings window available here; edit %s/config.json (or run trecs-notes -tui)", dir)
				openFolder(dir)
			}
			return
		}
		log.Printf("settings: %v", err)
		return
	}
	after, err := config.Load()
	if err != nil {
		log.Printf("settings: %v", err)
		return
	}
	a.Cfg = after
	// a changed hotkey is noticed by watchConfig, which restarts the app
}

func exitCodeOf(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// RunSettings is `trecs-notes settings` (internal): show the window, and save
// the config if it was saved. Exit 2 = no window available.
func RunSettings() int {
	form, err := notes.LoadForm()
	if err != nil {
		fmt.Fprintln(os.Stderr, "settings:", err)
		return 1
	}
	// The database is opened only when a button needs it: it takes a moment,
	// which made the window slow to appear. The tray process, which already
	// has it open, passes in the one number the window shows.
	var db *localdb.DB
	getDB := func() (*localdb.DB, error) {
		if db != nil {
			return db, nil
		}
		path, err := config.DBPath()
		if err != nil {
			return nil, err
		}
		db, err = localdb.Open(path)
		return db, err
	}
	defer func() {
		if db != nil {
			_ = db.Close()
		}
	}()
	unsynced := 0
	if n, err := strconv.Atoi(os.Getenv("TRECS_UNSYNCED")); err == nil {
		unsynced = n
	} else if d, err := getDB(); err == nil {
		unsynced, _ = d.CountUnsynced()
	}

	dbPath, _ := config.DBPath()
	data := ui.SettingsData{
		Hotkey: form.Hotkey, QuickHotkey: form.QuickHotkey, Width: form.Width, Height: form.Height,
		Remote: form.RemoteSyncEnabled, CopyFallback: form.CopyFallback,
		Server: form.Server, Email: form.Email, Password: form.Password,
		LoginStatus: notes.LoginStatus(), Unsynced: unsynced, DBPath: dbPath,
	}
	own := []string{form.Hotkey, form.QuickHotkey}
	h := func(action int, a1, a2, a3 string) ui.SettingsReply {
		var r notes.Reply
		switch action {
		case ui.ActLogin: // server, email, password
			r = notes.Login(a1, a2, a3)
		case ui.ActLogout:
			r = notes.Logout()
		case ui.ActSend:
			d, err := getDB()
			if err != nil {
				return ui.SettingsReply{Message: err.Error(), Unsynced: unsynced}
			}
			r = notes.SendUnsynced(d)
			unsynced, _ = d.CountUnsynced()
		case ui.ActOpenFolder:
			if dir, err := config.NotesDir(); err == nil {
				openFolder(dir)
			}
			r = notes.Reply{OK: true}
		case ui.ActCheckHotkey: // a1: the hotkey, a2: the other one
			r = checkHotkey(own, a1, a2)
		default:
			r = notes.Reply{Message: "unknown action"}
		}
		return ui.SettingsReply{Message: r.Message, LoginStatus: r.LoginStatus, Unsynced: unsynced, OK: r.OK}
	}
	res, err := ui.ShowSettings(data, h)
	if ui.IsUnavailable(err) {
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "settings:", err)
		return 1
	}
	if !res.Saved {
		return 0
	}
	form.Hotkey, form.QuickHotkey, form.Width, form.Height = res.Hotkey, res.QuickHotkey, res.Width, res.Height
	form.CopyFallback = res.CopyFallback
	form.RemoteSyncEnabled, form.Server, form.Email, form.Password = res.Remote, res.Server, res.Email, res.Password
	if err := notes.SaveForm(form); err != nil {
		fmt.Fprintln(os.Stderr, "settings:", err)
		return 1
	}
	return 0
}

// checkHotkey is the window's check of a new combination: it must parse,
// differ from the other one, and be free to grab (tried in a child process,
// so a failed grab cannot disturb this one).
func checkHotkey(own []string, spec, other string) notes.Reply {
	if other != "" && spec == other {
		return notes.Reply{Message: "the two hotkeys must differ"}
	}
	if slices.Contains(own, spec) { // held by this very app already
		return notes.Reply{OK: true}
	}
	if _, err := hotkey.Parse(spec); err != nil {
		return notes.Reply{Message: err.Error()}
	}
	exe, err := os.Executable()
	if err != nil {
		return notes.Reply{OK: true}
	}
	out, err := exec.Command(exe, "hotkey-check", spec).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" || strings.Contains(msg, "BadAccess") {
			msg = "another program already uses it"
		}
		return notes.Reply{Message: spec + " cannot be used: " + msg}
	}
	return notes.Reply{OK: true}
}

// HotkeyCheck is `trecs-notes hotkey-check SPEC` (internal): try to grab it.
func HotkeyCheck(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: trecs-notes hotkey-check SPEC")
		return 1
	}
	h, err := hotkey.Register(args[0], func() {})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	time.Sleep(300 * time.Millisecond)
	_ = h.Unregister()
	return 0
}

func openFolder(dir string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dir)
	case "windows":
		cmd = exec.Command("explorer", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("open folder: %v", err)
		return
	}
	go func() { _ = cmd.Wait() }()
}
