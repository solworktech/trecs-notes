package capture

import "testing"

func TestIsTerminal(t *testing.T) {
	for name, want := range map[string]bool{
		"wezterm": true, "WezTerm-gui": true, "gnome-terminal-server": true, "xterm": true,
		"WindowsTerminal": true, "cmd": true, "Terminal": true, "iTerm2": true, "ConsoleWindowClass": true,
		"CASCADIA_HOSTING_WINDOW_CLASS": true, "st": true, "st-256color": true,
		"firefox": false, "Firefox": false, "Mario": false, "code": false, "chrome": false, "Notepad": false,
		"Cmder-Notes": false, "": false, "strings": false, "starter": false,
	} {
		if got := IsTerminal(name); got != want {
			t.Errorf("IsTerminal(%q) = %v, want %v", name, got, want)
		}
	}
}
