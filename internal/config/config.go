// Package config loads and saves trecs-notes' own settings: the capture
// hotkey, and whether (and where) notes are also sent to a trecs server.
package config

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// BaseDir is the directory trecs-notes shares with the `trecs` recorder:
// $XDG_CONFIG_HOME/trecs or $HOME/trecs (see recorder/main.go's
// defaultOutputDir in the main trecs repo, which this mirrors exactly).
//
// NOTE for whoever packages the Windows build: this is the same logic the
// recorder already uses for ~/trecs/recordings, carried over unchanged so
// a note's local DB sits beside a recording's files as asked - but neither
// XDG_CONFIG_HOME nor HOME is reliably set in a plain Windows environment
// (cmd.exe/services do not export HOME; PowerShell sometimes does). This is
// a pre-existing gap in the recorder, not something new to trecs-notes: if
// $HOME is empty here, baseDir() falls back to os.UserHomeDir() (which does
// know how to find the profile directory on every platform, including
// Windows) rather than silently writing to the current directory. Flagging
// this because the recorder itself doesn't have that fallback yet - you may
// want it there too, for the same reason.
func BaseDir() (string, error) {
	if d := cmp.Or(os.Getenv("XDG_CONFIG_HOME"), os.Getenv("HOME")); d != "" {
		return filepath.Join(d, "trecs"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find a home directory: %w", err)
	}
	return filepath.Join(home, "trecs"), nil
}

// NotesDir is BaseDir()/notes: where the local database and this config file
// live. Kept separate from "recordings" so the two never collide on a name.
func NotesDir() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "notes"), nil
}

func configPath() (string, error) {
	dir, err := NotesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// DefaultHotkey is what a fresh install captures with. It is deliberately
// not a combination already claimed by a desktop environment (Ctrl+Shift+N
// opens Firefox/Chrome private/incognito windows in most of them), so first
// run doesn't silently fail to register.
const DefaultHotkey = "Ctrl+Alt+Space"

// DefaultQuickHotkey stores the selection straight away, without the form.
// Empty in the config turns the quick save off.
const DefaultQuickHotkey = "Ctrl+Alt+Shift+Space"

// DefaultServer is the server used when nothing else says which. A release
// build sets it exactly as trecs does:
//
//	go build -ldflags "-X main.defaultServer=https://trecs.run" ./cmd/trecs-notes
//
// (cmd/trecs-notes copies that variable here at start-up). Empty in a plain
// `go build`.
var DefaultServer = "https://trecs.run"

// Default size of the capture form, in pixels.
const (
	DefaultWidth  = 600
	DefaultHeight = 460
)

// DBPath is the local SQLite database.
func DBPath() (string, error) {
	dir, err := NotesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "notes.db"), nil
}

// Config is everything the person can change about how trecs-notes behaves.
type Config struct {
	// Hotkey is parsed by internal/hotkey.Parse. Configurable per the
	// spec, because global hotkeys are the one piece every desktop (and
	// every person) tends to already have opinions about.
	Hotkey string `json:"hotkey"`

	// QuickHotkey saves the current selection at once, with no tags and no
	// form; nothing happens if no text is selected. Empty = not bound.
	QuickHotkey string `json:"quick_hotkey"`

	// Width and Height are the capture form's default size.
	Width  int `json:"width"`
	Height int `json:"height"`

	// CopyFallback lets a capture send the copy shortcut to an application
	// that does not report its selection (see internal/capture). Off unless
	// the person turns it on: it types into their windows.
	CopyFallback bool `json:"copy_fallback"`

	// RemoteSyncEnabled turns on the optional second copy in the trecs DB.
	// When true, every save dual-writes: local SQLite always happens
	// first and is never blocked by the remote call failing.
	RemoteSyncEnabled bool `json:"remote_sync_enabled"`

	// Server is the trecs server to sync to, if the person chose one;
	// empty means "work it out" (see api.ResolveServer: the last login's,
	// else $TRECS_SERVER, else the built-in DefaultServer).
	Server string `json:"server,omitempty"`

	// Email is the login name for that server (the password is kept by
	// internal/credentials, never in this file).
	Email string `json:"email,omitempty"`
}

func defaults() Config {
	return Config{Hotkey: DefaultHotkey, QuickHotkey: DefaultQuickHotkey, Width: DefaultWidth, Height: DefaultHeight}
}

// Load reads the saved config, or returns the defaults if none has been
// saved yet (not an error: that's just a fresh install).
func Load() (Config, error) {
	path, err := configPath()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaults(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}
	cfg := defaults()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("%s is not valid: %w", path, err)
	}
	return cfg, nil
}

// Save writes the config, creating its directory if needed.
func (c Config) Save() error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
