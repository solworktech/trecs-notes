package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEditExternally(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the editor")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "ed.sh")
	// the "editor" appends a line, as an editor would end the file with a newline
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '\\nadded' >> \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := editExternally(script, "one")
	if err != nil || got != "one\nadded" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := editExternally(filepath.Join(dir, "missing"), "x"); err == nil {
		t.Error("a missing editor should fail")
	}
	if _, err := editExternally("false", "x"); err == nil {
		t.Error("an editor that fails should fail")
	}
	if _, err := editExternally("  ", "x"); err == nil {
		t.Error("no editor should fail")
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "nano -w")
	if editorCommand() != "nano -w" {
		t.Errorf("EDITOR: %q", editorCommand())
	}
	t.Setenv("VISUAL", "code -w")
	if editorCommand() != "code -w" {
		t.Errorf("VISUAL first: %q", editorCommand())
	}
}
