package config

import "testing"

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
