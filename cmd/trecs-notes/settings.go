package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/solworktech/trecs-notes/internal/api"
	"github.com/solworktech/trecs-notes/internal/config"
	"github.com/solworktech/trecs-notes/internal/credentials"
	"github.com/solworktech/trecs-notes/internal/hotkey"
	"github.com/solworktech/trecs-notes/internal/localdb"
	"github.com/solworktech/trecs-notes/internal/remote"
	"github.com/solworktech/trecs-notes/internal/ui"
)

var settingsOpen sync.Mutex

// openSettings runs the Settings window as a child process (GTK stays out of
// the tray process) and applies what it saved: the config file is the hand-
// off, so a changed hotkey is noticed by comparing before and after.
func (a *app) openSettings() {
	if !settingsOpen.TryLock() {
		return // already open
	}
	defer settingsOpen.Unlock()

	before, _ := config.Load()
	exe, err := os.Executable()
	if err != nil {
		log.Printf("settings: %v", err)
		return
	}
	cmd := exec.Command(exe, "settings")
	if n, err := a.db.CountUnsynced(); err == nil {
		cmd.Env = append(os.Environ(), "TRECS_UNSYNCED="+strconv.Itoa(n))
	}
	cmd.Stderr = os.Stderr
	if err := ui.RunChild(cmd); err != nil {
		if exitCodeOf(err) == 2 {
			// No settings window on this system: the config file is the
			// fallback, and it is plain JSON.
			if dir, derr := config.NotesDir(); derr == nil {
				log.Printf("no settings window available here; edit %s/config.json", dir)
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
	a.cfg = after
	if after.Hotkey != before.Hotkey {
		// The grab can't be swapped in place (see internal/hotkey: Unregister
		// blocks on Linux), so start a fresh copy and let this one go.
		log.Printf("hotkey changed to %s: restarting", after.Hotkey)
		next := exec.Command(exe)
		next.Stdout, next.Stderr = os.Stdout, os.Stderr
		if err := next.Start(); err != nil {
			log.Printf("restart: %v (start trecs-notes again to use the new hotkey)", err)
			return
		}
		ui.CloseDialog()
		_ = a.db.Close()
		os.Exit(0)
	}
}

func exitCodeOf(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// runSettingsProcess is `trecs-notes settings`: show the window, and save
// the config if it was saved. Exit 2 = no window available.
func runSettingsProcess() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "settings:", err)
		return 1
	}
	// The database is opened only when a button needs it: opening SQLite
	// (it is compiled to WebAssembly and loaded the first time) takes over a
	// second, which made the window slow to appear. The tray process, which
	// already has it open, passes in the one number the window shows.
	var db *localdb.DB
	getDB := func() (*localdb.DB, error) {
		if db != nil {
			return db, nil
		}
		dir, err := config.NotesDir()
		if err != nil {
			return nil, err
		}
		db, err = localdb.Open(dir + "/notes.db")
		return db, err
	}
	defer func() {
		if db != nil {
			_ = db.Close()
		}
	}()
	unsynced, haveCount := 0, false
	if n, err := strconv.Atoi(os.Getenv("TRECS_UNSYNCED")); err == nil {
		unsynced, haveCount = n, true
	} else if d, err := getDB(); err == nil {
		unsynced, _ = d.CountUnsynced()
		haveCount = true
	}
	_ = haveCount

	dbPath, _ := config.DBPath()
	stored, _ := credentials.Load()
	data := ui.SettingsData{
		Hotkey: cfg.Hotkey, Width: cfg.Width, Height: cfg.Height,
		Remote: cfg.RemoteSyncEnabled, CopyFallback: cfg.CopyFallback,
		Server:      firstNonEmpty(cfg.Server, stored.Server, api.ResolveServer("")),
		Email:       firstNonEmpty(stored.Email, cfg.Email),
		Password:    stored.Password,
		LoginStatus: loginStatus(), Unsynced: unsynced, DBPath: dbPath,
	}
	h := func(action int, a1, a2, a3 string) ui.SettingsReply {
		var r ui.SettingsReply
		if action == ui.ActSend {
			d, err := getDB()
			if err != nil {
				return ui.SettingsReply{Message: err.Error(), Unsynced: unsynced}
			}
			r = settingsAction(d, cfg.Hotkey, action, a1, a2, a3)
			unsynced, _ = d.CountUnsynced()
		} else {
			r = settingsAction(nil, cfg.Hotkey, action, a1, a2, a3)
		}
		r.Unsynced = unsynced
		return r
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
	cfg.Hotkey, cfg.Width, cfg.Height = res.Hotkey, res.Width, res.Height
	cfg.CopyFallback = res.CopyFallback
	cfg.RemoteSyncEnabled, cfg.Server = res.Remote, strings.TrimSpace(res.Server)
	cfg.Email = strings.TrimSpace(res.Email)
	if cfg.Email != "" && res.Password != "" {
		server := firstNonEmpty(cfg.Server, api.ResolveServer(""))
		if _, err := credentials.Save(credentials.Login{Server: server, Email: cfg.Email, Password: res.Password}); err != nil {
			fmt.Fprintln(os.Stderr, "settings: storing the login:", err)
		}
	}
	if err := cfg.Save(); err != nil {
		fmt.Fprintln(os.Stderr, "settings:", err)
		return 1
	}
	return 0
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

func loginStatus() string {
	if sess, err := api.LoadSession(); err == nil {
		return "Logged in to " + sess.Server
	}
	return "Not logged in"
}

func settingsAction(db *localdb.DB, currentHotkey string, action int, a1, a2, a3 string) ui.SettingsReply {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switch action {
	case ui.ActLogin: // server, email, password
		server, err := api.NormalizeServer(firstNonEmpty(strings.TrimSpace(a1), api.ResolveServer("")))
		if err != nil {
			return ui.SettingsReply{Message: err.Error()}
		}
		if strings.TrimSpace(a2) == "" || a3 == "" {
			return ui.SettingsReply{Message: "Enter your email and password."}
		}
		sess, err := api.Login(ctx, server, strings.TrimSpace(a2), a3)
		if err != nil {
			return ui.SettingsReply{Message: "Login failed: " + err.Error()}
		}
		if err := sess.Save(); err != nil {
			return ui.SettingsReply{Message: "Logged in, but the session could not be saved: " + err.Error()}
		}
		where, serr := credentials.Save(credentials.Login{Server: server, Email: strings.TrimSpace(a2), Password: a3})
		msg := "Logged in. Password kept in " + where + "."
		if serr != nil {
			msg = "Logged in, but the password could not be stored: " + serr.Error()
		}
		return ui.SettingsReply{Message: msg, LoginStatus: loginStatus(), OK: true}
	case ui.ActLogout:
		if sess, err := api.LoadSession(); err == nil {
			_ = api.FromSession(sess).Logout(ctx) // best effort: the local session goes either way
		}
		if err := api.DeleteSession(); err != nil {
			return ui.SettingsReply{Message: err.Error()}
		}
		credentials.Delete()
		return ui.SettingsReply{Message: "Logged out. The stored password was removed.", LoginStatus: loginStatus(), OK: true}
	case ui.ActSend:
		sent, failed, lastErr := remote.SendUnsynced(db, 500)
		return ui.SettingsReply{Message: remote.Describe(sent, failed, lastErr), OK: failed == 0}
	case ui.ActOpenFolder:
		if dir, err := config.NotesDir(); err == nil {
			openFolder(dir)
		}
		return ui.SettingsReply{OK: true}
	case ui.ActCheckHotkey:
		if a1 == currentHotkey {
			return ui.SettingsReply{OK: true}
		}
		if _, _, err := hotkey.Parse(a1); err != nil {
			return ui.SettingsReply{Message: err.Error()}
		}
		exe, err := os.Executable()
		if err != nil {
			return ui.SettingsReply{OK: true}
		}
		// Tried in a child process, so a failed grab can't disturb this one.
		out, err := exec.Command(exe, "hotkey-check", a1).CombinedOutput()
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" || strings.Contains(msg, "BadAccess") {
				msg = "another program already uses it"
			}
			return ui.SettingsReply{Message: a1 + " cannot be used: " + msg}
		}
		return ui.SettingsReply{OK: true}
	}
	return ui.SettingsReply{Message: "unknown action"}
}

// runHotkeyCheck is `trecs-notes hotkey-check SPEC`: try to grab it. It
// exits without Unregister on purpose (see internal/hotkey).
func runHotkeyCheck(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: trecs-notes hotkey-check SPEC")
		return 1
	}
	if _, err := hotkey.Register(args[0], func() {}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	time.Sleep(300 * time.Millisecond)
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
