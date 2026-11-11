package notes

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/solworktech/trecs-notes/internal/api"
	"github.com/solworktech/trecs-notes/internal/config"
	"github.com/solworktech/trecs-notes/internal/credentials"
	"github.com/solworktech/trecs-notes/internal/hotkey"
	"github.com/solworktech/trecs-notes/internal/localdb"
	"github.com/solworktech/trecs-notes/internal/remote"
)

// Limits of the capture form's size, in pixels.
const (
	MinWidth, MaxWidth   = 300, 2400
	MinHeight, MaxHeight = 250, 1800
)

// Reply is the outcome of a settings action, for a frontend to show.
type Reply struct {
	Message     string
	LoginStatus string // set when the action changed it
	OK          bool
}

// Form is the settings as a person sees and edits them: the config file, plus
// what is kept elsewhere (the password lives in the system keyring).
type Form struct {
	config.Config
	Password string
}

// LoadForm is what a settings screen opens with. The server, email and
// password are filled from the saved login when the config has none.
func LoadForm() (Form, error) {
	cfg, err := config.Load()
	if err != nil {
		return Form{}, err
	}
	stored, _ := credentials.Load()
	cfg.Server = firstNonEmpty(cfg.Server, stored.Server, api.ResolveServer(""))
	cfg.Email = firstNonEmpty(stored.Email, cfg.Email)
	return Form{Config: cfg, Password: stored.Password}, nil
}

// ValidateHotkeys checks the two hotkeys of a settings screen: each has to
// parse (quick may be empty, which turns it off), and they must differ.
func ValidateHotkeys(capture, quick string) error {
	if _, err := hotkey.Parse(capture); err != nil {
		return err
	}
	if quick == "" {
		return nil
	}
	if _, err := hotkey.Parse(quick); err != nil {
		return err
	}
	if capture == quick {
		return fmt.Errorf("the two hotkeys must differ")
	}
	return nil
}

// Validate says what is wrong with a form, if anything.
func (f Form) Validate() error {
	if err := ValidateHotkeys(f.Hotkey, f.QuickHotkey); err != nil {
		return err
	}
	if f.Width < MinWidth || f.Width > MaxWidth {
		return fmt.Errorf("the form width must be %d to %d", MinWidth, MaxWidth)
	}
	if f.Height < MinHeight || f.Height > MaxHeight {
		return fmt.Errorf("the form height must be %d to %d", MinHeight, MaxHeight)
	}
	return nil
}

// SaveForm writes the settings to the config file, and the login (when an
// email and a password were given) to the keyring. The config file is what
// the running tray app and every other frontend read; none is told.
func SaveForm(f Form) error {
	if err := f.Validate(); err != nil {
		return err
	}
	f.Server, f.Email = strings.TrimSpace(f.Server), strings.TrimSpace(f.Email)
	var credErr error
	if f.Email != "" && f.Password != "" {
		server := firstNonEmpty(f.Server, api.ResolveServer(""))
		_, credErr = credentials.Save(credentials.Login{Server: server, Email: f.Email, Password: f.Password})
	}
	if err := f.Save(); err != nil {
		return err
	}
	if credErr != nil {
		return fmt.Errorf("saved, but the login could not be stored: %w", credErr)
	}
	return nil
}

// HotkeysChanged says whether a running app has to be restarted to pick the
// settings up: its hotkeys are grabbed when it starts.
func HotkeysChanged(before, after config.Config) bool {
	return before.Hotkey != after.Hotkey || before.QuickHotkey != after.QuickHotkey
}

// LoginStatus is a line saying who the notes are sent as.
func LoginStatus() string {
	if sess, err := api.LoadSession(); err == nil {
		return "Logged in to " + sess.Server
	}
	return "Not logged in"
}

// Login logs in to the server and keeps the session and the password.
func Login(server, email, password string) Reply {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	server, err := api.NormalizeServer(firstNonEmpty(strings.TrimSpace(server), api.ResolveServer("")))
	if err != nil {
		return Reply{Message: err.Error()}
	}
	email = strings.TrimSpace(email)
	if email == "" || password == "" {
		return Reply{Message: "Enter your email and password."}
	}
	sess, err := api.Login(ctx, server, email, password)
	if err != nil {
		return Reply{Message: "Login failed: " + err.Error()}
	}
	if err := sess.Save(); err != nil {
		return Reply{Message: "Logged in, but the session could not be saved: " + err.Error()}
	}
	where, serr := credentials.Save(credentials.Login{Server: server, Email: email, Password: password})
	msg := "Logged in. Password kept in " + where + "."
	if serr != nil {
		msg = "Logged in, but the password could not be stored: " + serr.Error()
	}
	return Reply{Message: msg, LoginStatus: LoginStatus(), OK: true}
}

// Logout ends the session and removes the stored password.
func Logout() Reply {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if sess, err := api.LoadSession(); err == nil {
		_ = api.FromSession(sess).Logout(ctx) // best effort: the local session goes either way
	}
	if err := api.DeleteSession(); err != nil {
		return Reply{Message: err.Error()}
	}
	credentials.Delete()
	return Reply{Message: "Logged out. The stored password was removed.", LoginStatus: LoginStatus(), OK: true}
}

// SyncNow syncs both ways: sends what the server has not seen, then takes in
// what it has that this machine has not (notes edited or deleted on the
// website or on another machine).
func SyncNow(db *localdb.DB) Reply {
	sent, failed, pulled, lastErr := remote.Sync(db)
	return Reply{Message: remote.DescribeSync(sent, failed, pulled, lastErr), OK: failed == 0}
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
