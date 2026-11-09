package capture

import (
	"testing"
)

func TestQuickNote(t *testing.T) {
	for _, text := range []string{"", "   ", "\n\t \n"} {
		snap := Snapshot{AppName: "Code", SelectedText: text}
		if _, ok := snap.QuickNote(); ok {
			t.Errorf("text %q: stored a note, want nothing", text)
		}
	}
	snap := Snapshot{AppName: "Code", WindowTitle: "main.go", SelectedText: " x := 1\n"}
	n, ok := snap.QuickNote()
	if !ok || n.AppName != "Code" || n.WindowTitle != "main.go" || n.Text != " x := 1\n" || len(n.Tags) != 0 {
		t.Errorf("got %+v, %v", n, ok)
	}
}
