//go:build darwin

package ui

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// showCaptureDialog on macOS: vanilla AppleScript's "display dialog" has
// only one editable text field per call (no multi-field form), so this is
// a short chain of dialogs rather than the single modal yad gives on
// Linux or Windows Forms gives on Windows - the same trade-off noted in
// the package doc. Application and window title are shown together,
// read-only, in the first confirmation dialog (editing them separately
// gains little - they're metadata, not the thing being saved); Tags and
// Text each get their own editable dialog.
func showCaptureDialog(in Input) (Result, error) {
	hint := ""
	if len(in.TagSuggestions) > 0 {
		hint = "\n\nTags used before: " + strings.Join(in.TagSuggestions, ", ")
	}
	confirmScript := fmt.Sprintf(`display dialog %s buttons {"Cancel", "Continue"} default button "Continue" with title "Trecs Notes"`,
		asQuoted(fmt.Sprintf("App: %s\nTitle: %s%s", in.Snapshot.AppName, in.Snapshot.WindowTitle, hint)))
	if _, err := osascript(confirmScript); cancelled(err) {
		return Result{}, nil
	} else if err != nil {
		return Result{}, err
	}

	tagsScript := fmt.Sprintf(`text returned of (display dialog "Tags (space or comma separated):" default answer "" buttons {"Cancel", "Continue"} default button "Continue" with title "Trecs Notes")`)
	tags, err := osascript(tagsScript)
	if cancelled(err) {
		return Result{}, nil
	} else if err != nil {
		return Result{}, err
	}

	textScript := fmt.Sprintf(`text returned of (display dialog "Note:" default answer %s buttons {"Cancel", "Save"} default button "Save" with title "Trecs Notes")`,
		asQuoted(in.Snapshot.SelectedText))
	text, err := osascript(textScript)
	if cancelled(err) {
		return Result{}, nil
	} else if err != nil {
		return Result{}, err
	}

	return Result{
		AppName:     in.Snapshot.AppName,
		WindowTitle: in.Snapshot.WindowTitle,
		TagsRaw:     strings.TrimSpace(tags),
		Text:        text,
		Confirmed:   true,
	}, nil
}

// asQuoted renders s as an AppleScript string literal: escapes backslash
// and double-quote, which is all an AppleScript string needs.
func asQuoted(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// cancelled reports whether osascript's error is just "the person clicked
// Cancel" (osascript's "User canceled." runtime error, exit status 1) as
// opposed to a real failure.
func cancelled(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "User canceled") || exitCode(err) == 1
}

func exitCode(err error) int {
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

func osascript(script string) (string, error) {
	var out, errOut bytes.Buffer
	cmd := exec.Command("osascript", "-e", script)
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := runTracked(cmd)
	if err != nil && errOut.Len() > 0 {
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(errOut.String()))
	}
	return strings.TrimRight(out.String(), "\n"), err
}
