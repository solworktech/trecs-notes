package hotkey

import "testing"

func TestParse(t *testing.T) {
	s, err := Parse("Ctrl+Alt+Shift+Space")
	if err != nil || !s.Ctrl || !s.Alt || !s.Shift || s.Super || s.Key != "SPACE" {
		t.Errorf("got %+v, %v", s, err)
	}
	if s, err := Parse("Super+f7"); err != nil || !s.Super || s.Key != "F7" {
		t.Errorf("got %+v, %v", s, err)
	}
	for _, bad := range []string{"Space", "Ctrl", "Ctrl+Hyper+A", "Ctrl+F13", "Ctrl+F0", "Ctrl+Tab", "Ctrl+AB", "Ctrl+F1x"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q should not parse", bad)
		}
	}
}
