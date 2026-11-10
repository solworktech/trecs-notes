package notes

import (
	"testing"

	"github.com/solworktech/trecs-notes/internal/config"
)

func TestValidateHotkeys(t *testing.T) {
	for _, c := range []struct {
		hk, quick string
		ok        bool
	}{
		{"Ctrl+Alt+Space", "Ctrl+Alt+Shift+Space", true},
		{"Ctrl+Alt+Space", "", true}, // quick save off
		{"Ctrl+Alt+Space", "Ctrl+Alt+Space", false},
		{"Space", "", false},
		{"Ctrl+Alt+Space", "Ctrl+Nope", false},
	} {
		if err := ValidateHotkeys(c.hk, c.quick); (err == nil) != c.ok {
			t.Errorf("%q, %q: %v", c.hk, c.quick, err)
		}
	}
}

func TestFormValidateAndRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f, err := LoadForm()
	if err != nil {
		t.Fatal(err)
	}
	if f.Hotkey != config.DefaultHotkey || f.Server == "" {
		t.Fatalf("defaults: %+v", f)
	}
	bad := f
	bad.Width = 10
	if bad.Validate() == nil {
		t.Error("a 10 pixel wide form should be refused")
	}
	f.Width, f.Height, f.QuickHotkey = 700, 500, ""
	if err := SaveForm(f); err != nil {
		t.Fatal(err)
	}
	g, err := config.Load()
	if err != nil || g.Width != 700 || g.Height != 500 || g.QuickHotkey != "" {
		t.Errorf("saved config: %+v, %v", g, err)
	}
	if !HotkeysChanged(config.Config{Hotkey: "Ctrl+A"}, config.Config{Hotkey: "Ctrl+B"}) || HotkeysChanged(g, g) {
		t.Error("HotkeysChanged")
	}
}
