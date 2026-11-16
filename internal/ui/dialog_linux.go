//go:build linux

package ui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// errGTKUnavailable: GTK could not start (no display, or built without it).
var errGTKUnavailable = errors.New("GTK is not available")

// RunDialogProcess is the body of `trecs-notes dialog`: read an Input as
// JSON on stdin, show the GTK form, write the Result as JSON on stdout.
// It runs as a child process so GTK, its main loop and any display problem
// stay out of the tray process. Exit code 2 means GTK could not start.
func RunDialogProcess() int {
	var in Input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fmt.Fprintln(os.Stderr, "dialog:", err)
		return 1
	}
	res, err := runGTK(in)
	if errors.Is(err, errGTKUnavailable) {
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "dialog:", err)
		return 1
	}
	_ = json.NewEncoder(os.Stdout).Encode(res)
	return 0
}

func showGTKSubprocess(in Input) (Result, error) {
	exe, err := os.Executable()
	if err != nil {
		return Result{}, errGTKUnavailable
	}
	payload, _ := json.Marshal(in)
	cmd := exec.Command(exe, "dialog")
	cmd.Stdin = bytes.NewReader(payload)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := runTracked(cmd); err != nil {
		if exitCode(err) == 2 {
			return Result{}, errGTKUnavailable
		}
		return Result{}, err
	}
	var res Result
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		return Result{}, err
	}
	return res, nil
}

// cancelled is returned by the OS tool on Cancel/Esc (exit code 1) - not a
// real error, just "the person backed out", which showCaptureDialog turns
// into Result{Confirmed: false}.
const cancelledExit = 1

func showCaptureDialog(in Input) (Result, error) {
	// One real form with every field at once, via GTK in a child process;
	// yad and zenity are only fallbacks for a machine where GTK cannot run.
	res, err := showGTKSubprocess(in)
	if !errors.Is(err, errGTKUnavailable) {
		return res, err
	}
	if commandExists("yad") {
		return showYad(in)
	}
	if commandExists("zenity") {
		return showZenityChain(in)
	}
	return Result{}, fmt.Errorf("neither yad nor zenity is installed; trecs-notes needs one of them for the capture dialog on Linux")
}

// showYad puts up one real multi-field form - yad's --field supports
// default values and a multi-line text area (TXT), unlike plain zenity, so
// this is the one Linux path that matches the spec's "small modal with
// fields" as a single dialog.
func showYad(in Input) (Result, error) {
	hint := ""
	if len(in.TagSuggestions) > 0 {
		hint = "used before: " + strings.Join(in.TagSuggestions, ", ")
	}
	args := []string{
		"--form", "--title=Trecs Notes", "--width=520", "--height=420",
		"--field=App", in.Snapshot.AppName,
		"--field=Title", in.Snapshot.WindowTitle,
		"--field=Tags (" + hint + "):", "",
		"--field=Note:TXT", in.Snapshot.SelectedText,
	}
	out, err := run("yad", args...)
	if exitCode(err) == cancelledExit {
		return Result{Confirmed: false}, nil
	}
	if err != nil {
		return Result{}, err
	}
	// yad --form prints the fields "|"-separated, one line.
	parts := strings.Split(strings.TrimRight(out, "\n"), "|")
	for len(parts) < 4 {
		parts = append(parts, "")
	}
	return Result{
		AppName:     parts[0],
		WindowTitle: parts[1],
		TagsRaw:     parts[2],
		Text:        parts[3],
		Confirmed:   true,
	}, nil
}

// showZenityChain is the fallback when yad isn't installed: zenity has no
// form field that accepts a default value, so a single --forms call can't
// be pre-filled with the capture. Instead: one entry dialog per short field
// (pre-filled via --entry-text, which plain --entry does support), then one
// editable text-info dialog for the body. Four small dialogs instead of
// one - a real UX cost, flagged in the package doc; yad above avoids it.
func showZenityChain(in Input) (Result, error) {
	appName, err := zenityEntry("App", in.Snapshot.AppName)
	if cancelled(err) {
		return Result{}, nil
	} else if err != nil {
		return Result{}, err
	}

	winTitle, err := zenityEntry("Title", in.Snapshot.WindowTitle)
	if cancelled(err) {
		return Result{}, nil
	} else if err != nil {
		return Result{}, err
	}

	hint := "Tags, space or comma separated"
	if len(in.TagSuggestions) > 0 {
		hint += " (used before: " + strings.Join(in.TagSuggestions, ", ") + ")"
	}
	tags, err := zenityEntry(hint, "")
	if cancelled(err) {
		return Result{}, nil
	} else if err != nil {
		return Result{}, err
	}

	text, err := zenityTextInfo(in.Snapshot.SelectedText)
	if cancelled(err) {
		return Result{}, nil
	} else if err != nil {
		return Result{}, err
	}

	return Result{AppName: appName, WindowTitle: winTitle, TagsRaw: tags, Text: text, Confirmed: true}, nil
}

func zenityEntry(label, prefill string) (string, error) {
	out, err := run("zenity", "--entry", "--title=Trecs Notes", "--text="+label+":", "--entry-text="+prefill, "--width=480")
	return strings.TrimRight(out, "\n"), err
}

func zenityTextInfo(body string) (string, error) {
	tmp, err := os.CreateTemp("", "trecs-notes-*.txt")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(body); err != nil {
		_ = tmp.Close()
		return "", err
	}
	_ = tmp.Close()
	out, err := run("zenity", "--text-info", "--editable", "--title=Trecs Notes - note",
		"--filename="+tmp.Name(), "--width=600", "--height=400")
	return out, err
}

func cancelled(err error) bool { return exitCode(err) == cancelledExit }

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func run(name string, args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &out
	err := runTracked(cmd)
	return out.String(), err
}

// RunSecretsProcess is the body of `trecs-notes secrets-dialog`: the question
// is read from stdin, and the answer ("mask", "keep" or "cancel") is written
// to stdout. Like the capture form it runs as a child process, so GTK stays
// out of the tray process; exit code 2 means GTK could not start.
func RunSecretsProcess() int {
	msg, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "secrets-dialog:", err)
		return 1
	}
	c, err := runGTKSecrets(string(msg))
	if errors.Is(err, errGTKUnavailable) {
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "secrets-dialog:", err)
		return 1
	}
	fmt.Println(secretWords[c])
	return 0
}

var secretWords = map[SecretChoice]string{SecretCancel: "cancel", SecretMask: "mask", SecretKeep: "keep"}

func askSecrets(msg string) (SecretChoice, error) {
	c, err := askSecretsGTK(msg)
	if !errors.Is(err, errGTKUnavailable) {
		return c, err
	}
	if commandExists("yad") {
		return askSecretsYad(msg)
	}
	if commandExists("zenity") {
		return askSecretsZenity(msg)
	}
	return SecretCancel, fmt.Errorf("neither yad nor zenity is installed, and GTK could not start")
}

func askSecretsGTK(msg string) (SecretChoice, error) {
	exe, err := os.Executable()
	if err != nil {
		return SecretCancel, errGTKUnavailable
	}
	cmd := exec.Command(exe, "secrets-dialog")
	cmd.Stdin = strings.NewReader(msg)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := runTracked(cmd); err != nil {
		if exitCode(err) == 2 {
			return SecretCancel, errGTKUnavailable
		}
		return SecretCancel, err
	}
	switch strings.TrimSpace(out.String()) {
	case "mask":
		return SecretMask, nil
	case "keep":
		return SecretKeep, nil
	}
	return SecretCancel, nil
}

// markup escapes text for yad and zenity, whose --text is Pango markup.
func markup(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func askSecretsYad(msg string) (SecretChoice, error) {
	_, err := run("yad", "--title=Trecs Notes", "--image=dialog-warning", "--text="+markup(msg), "--width=480",
		"--button=Mask and save:0", "--button=Save as is:2", "--button=Cancel:1")
	switch exitCode(err) {
	case 0:
		return SecretMask, nil
	case 2:
		return SecretKeep, nil
	case 1, 252: // Cancel, or the window closed
		return SecretCancel, nil
	}
	return SecretCancel, err
}

// askSecretsZenity: a question dialog has two buttons, so "Save as is" is an
// extra button, which zenity answers by printing its label and exiting 1.
func askSecretsZenity(msg string) (SecretChoice, error) {
	out, err := run("zenity", "--question", "--title=Trecs Notes", "--text="+markup(msg), "--width=480",
		"--ok-label=Mask and save", "--cancel-label=Cancel", "--extra-button=Save as is")
	if err == nil {
		return SecretMask, nil
	}
	if strings.Contains(out, "Save as is") {
		return SecretKeep, nil
	}
	if exitCode(err) == 1 {
		return SecretCancel, nil
	}
	return SecretCancel, err
}
