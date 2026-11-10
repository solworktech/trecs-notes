package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// editorCommand is the person's editor: $VISUAL, else $EDITOR, else vi.
func editorCommand() string {
	for _, k := range []string{"VISUAL", "EDITOR"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return "vi"
}

// editExternally lets editor (a command line, so "code -w" works) change
// text, in a temporary file that only its owner can read, and returns what the
// file holds afterwards. The terminal must already be the editor's to use.
func editExternally(editor, text string) (string, error) {
	argv := strings.Fields(editor)
	if len(argv) == 0 {
		return "", errors.New("no editor: set $VISUAL or $EDITOR")
	}
	f, err := os.CreateTemp("", "trecs-note-*.txt") // 0600
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.WriteString(text); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	cmd := exec.Command(argv[0], append(argv[1:], f.Name())...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w", argv[0], err)
	}
	b, err := os.ReadFile(f.Name())
	if err != nil {
		return "", err
	}
	out := string(b)
	// editors end a file with a newline the note did not have
	if !strings.HasSuffix(text, "\n") {
		out = strings.TrimSuffix(strings.TrimSuffix(out, "\n"), "\r")
	}
	return out, nil
}
