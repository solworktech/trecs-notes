// Package ui shows the capture modal: the fields from a capture.Snapshot,
// editable, plus tags, before a note is saved.
//
// Deliberately not built on a Go GUI toolkit (no Fyne, no Walk): each
// platform already ships a scripting layer that can put up a real native
// dialog - zenity/yad on Linux, AppleScript's display dialog on macOS,
// System.Windows.Forms via PowerShell on Windows - and using those keeps
// trecs-notes free of a GUI toolkit's build weight and its own cross-
// platform quirks. The cost, spelled out per platform below, is that the
// result isn't a single uniform form everywhere: Windows gets one real
// multi-field modal; Linux gets one if yad is installed, otherwise a
// sequence of simpler zenity dialogs; macOS gets a short sequence of
// AppleScript dialogs (vanilla AppleScript has no native multi-field form).
// If that unevenness turns out to matter more than the dependency savings,
// switching this one package to a GUI toolkit later is a contained change -
// nothing outside internal/ui would need to know.
package ui

import (
	"os/exec"
	"sync"

	"github.com/solworktech/trecs-notes/internal/capture"
)

// Input is what the dialog is shown with: the capture, and tags already in
// use (for the person to pick from; see the package doc about what each
// platform can actually do with that list - Windows can offer a real
// autocomplete-ish combo, the others just print them as a hint).
type Input struct {
	Snapshot       capture.Snapshot
	TagSuggestions []string
	Width, Height  int // form size in pixels; 0 = built-in default
}

// Result is what the person confirmed, or Confirmed=false if they
// cancelled - in which case nothing should be saved.
type Result struct {
	AppName     string
	WindowTitle string
	Text        string
	TagsRaw     string // free-text; split with model.SplitTagInput and validated with model.NormalizeTags by the caller
	Confirmed   bool
}

// ShowCaptureDialog is implemented per-OS (dialog_linux.go, dialog_darwin.go,
// dialog_windows.go).
func ShowCaptureDialog(in Input) (Result, error) {
	return showCaptureDialog(in)
}

var (
	dlgMu  sync.Mutex
	dlgCmd *exec.Cmd
)

// runTracked runs cmd to completion like cmd.Run, remembering it so
// CloseDialog can end it while it is open.
func runTracked(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	dlgMu.Lock()
	dlgCmd = cmd
	dlgMu.Unlock()
	err := cmd.Wait()
	dlgMu.Lock()
	dlgCmd = nil
	dlgMu.Unlock()
	return err
}

// CloseDialog closes the capture dialog if one is open (used by Quit, so
// the form does not outlive the app). Safe to call at any time.
func CloseDialog() {
	dlgMu.Lock()
	defer dlgMu.Unlock()
	if dlgCmd != nil && dlgCmd.Process != nil {
		_ = dlgCmd.Process.Kill()
	}
}

// RunChild runs a helper process (the Settings window) as the one tracked
// dialog, so Quit can close it.
func RunChild(cmd *exec.Cmd) error { return runTracked(cmd) }
