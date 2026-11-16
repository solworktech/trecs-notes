package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQuickHotkeyDefaultsAndCanBeTurnedOff(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, err := Load()
	if err != nil || c.QuickHotkey != DefaultQuickHotkey {
		t.Fatalf("fresh install: %q, %v", c.QuickHotkey, err)
	}
	c.QuickHotkey = ""
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if c, _ = Load(); c.QuickHotkey != "" {
		t.Errorf("an emptied quick hotkey came back as %q", c.QuickHotkey)
	}
}

func TestSecretCheckIsOnUnlessTurnedOff(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, err := Load()
	if err != nil || !c.SecretCheck {
		t.Fatalf("fresh install: %v, %v", c.SecretCheck, err)
	}
	// a config file written before the setting existed keeps it on
	path, _ := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"hotkey":"Ctrl+Alt+Space"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, _ = Load(); !c.SecretCheck {
		t.Error("an older config turned it off")
	}
	c.SecretCheck = false
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if c, _ = Load(); c.SecretCheck {
		t.Error("false came back as true")
	}
}
