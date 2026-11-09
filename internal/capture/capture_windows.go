//go:build windows

package capture

import (
	"bytes"
	"encoding/base64"
	"os/exec"
	"strings"
	"unicode/utf16"
)

// capture is the Windows implementation, through PowerShell (which ships with
// Windows, so no Go binding to the Win32 API has to be kept in step).
//
// The selection is read with UI Automation's TextPattern, the interface
// screen readers use: the focused element is asked for its current selection.
// No keystroke is sent and the clipboard is not touched. It works in Notepad
// and other standard text controls, Word, Windows Terminal and applications
// that implement accessibility; Chromium-based browsers and Electron
// applications expose it once an assistive client asks, so the first capture
// in one may come back empty.
//
// With Options.CopyFallback on, an application that gave no selection is sent
// Ctrl+C, never into a console or terminal (where it would interrupt the
// running program). The result is trusted only if the clipboard sequence
// number moved, and the previous text is put back; if the clipboard holds
// anything but plain text, nothing is sent.
func capture(opts Options) Snapshot {
	var s Snapshot

	out, err := runPowerShell(false, uiaScript)
	if err != nil {
		s.Partial = true
		s.Warnings = append(s.Warnings, "window and selection: "+err.Error())
		return s
	}
	out = strings.ReplaceAll(out, "\r\n", "\n")
	parts := strings.SplitN(out, "\n", 4)
	for len(parts) < 4 {
		parts = append(parts, "")
	}
	s.AppName, s.WindowTitle = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	class := strings.TrimSpace(parts[2])
	s.SelectedText = strings.TrimSuffix(parts[3], "\n")
	if s.SelectedText != "" {
		return s
	}

	s.Partial = true
	switch {
	case !opts.CopyFallback:
		s.Warnings = append(s.Warnings, "nothing selected, or this application does not report its selection (the copy fallback is off in Settings)")
	case IsTerminal(s.AppName) || IsTerminal(class):
		s.Warnings = append(s.Warnings, "nothing reported as selected, and not sending Ctrl+C to a terminal")
	default:
		text, warn := windowsCopyFallback()
		s.SelectedText = text
		if warn != "" {
			s.Warnings = append(s.Warnings, warn)
		}
	}
	return s
}

// psPrelude makes PowerShell write UTF-8 without a byte-order mark: its
// default is the console's legacy code page, which garbles anything but
// plain ASCII on its way back to Go.
const psPrelude = `
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false
$OutputEncoding = [Console]::OutputEncoding
$ErrorActionPreference = 'Stop'
`

// uiaScript prints, one per line: the foreground window's process name (not
// the exe path, so it reads "firefox" like on the other systems), its title,
// its window class, then the focused element's selection (which may itself
// span lines).
const uiaScript = psPrelude + `
Add-Type @"
using System;
using System.Runtime.InteropServices;
using System.Text;
public class TrecsNotesWin32 {
    [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern int GetWindowText(IntPtr hWnd, StringBuilder text, int count);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern int GetClassName(IntPtr hWnd, StringBuilder text, int count);
    [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr hWnd, out uint processId);
}
"@
$hwnd = [TrecsNotesWin32]::GetForegroundWindow()
$title = New-Object System.Text.StringBuilder 1024
[void][TrecsNotesWin32]::GetWindowText($hwnd, $title, $title.Capacity)
$class = New-Object System.Text.StringBuilder 256
[void][TrecsNotesWin32]::GetClassName($hwnd, $class, $class.Capacity)
$procId = 0
[void][TrecsNotesWin32]::GetWindowThreadProcessId($hwnd, [ref]$procId)
$procName = ""
try { $procName = (Get-Process -Id $procId).ProcessName } catch {}

$sel = ""
try {
    Add-Type -AssemblyName UIAutomationClient
    Add-Type -AssemblyName UIAutomationTypes
    $el = [System.Windows.Automation.AutomationElement]::FocusedElement
    $pattern = $null
    if ($el -ne $null -and $el.TryGetCurrentPattern([System.Windows.Automation.TextPattern]::Pattern, [ref]$pattern)) {
        $sel = (($pattern.GetSelection() | ForEach-Object { $_.GetText(100000) }) -join "` + "`n" + `")
    }
} catch {}

[Console]::Out.Write($procName + "` + "`n" + `" + $title.ToString() + "` + "`n" + `" + $class.ToString() + "` + "`n" + `" + $sel)
`

// copyScript sends Ctrl+C and prints OK and the copied text, or SKIP and the
// reason. It must run in an STA thread (the clipboard classes require it).
const copyScript = psPrelude + `
Add-Type -AssemblyName System.Windows.Forms
Add-Type @"
using System.Runtime.InteropServices;
public class TrecsNotesClip {
    [DllImport("user32.dll")] public static extern uint GetClipboardSequenceNumber();
}
"@
$data = [System.Windows.Forms.Clipboard]::GetDataObject()
$saved = ""
if ($data -ne $null) {
    $plain = @('System.String', 'UnicodeText', 'Text', 'OEMText', 'Locale')
    foreach ($f in $data.GetFormats()) {
        if ($plain -notcontains $f) { [Console]::Out.Write("SKIP` + "`n" + `the clipboard holds something other than text, which cannot be put back after a copy: not copying"); exit }
    }
    if ($data.GetDataPresent([System.Windows.Forms.DataFormats]::UnicodeText)) {
        $saved = [string]$data.GetData([System.Windows.Forms.DataFormats]::UnicodeText)
    }
}
$before = [TrecsNotesClip]::GetClipboardSequenceNumber()
[System.Windows.Forms.SendKeys]::SendWait("^c")
$changed = $false
for ($i = 0; $i -lt 25 -and -not $changed; $i++) {
    Start-Sleep -Milliseconds 20
    $changed = ([TrecsNotesClip]::GetClipboardSequenceNumber() -ne $before)
}
if (-not $changed) { [Console]::Out.Write("SKIP` + "`n" + `nothing selected (the copy did not change the clipboard)"); exit }
$text = ""
if ([System.Windows.Forms.Clipboard]::ContainsText()) { $text = [System.Windows.Forms.Clipboard]::GetText() }
if ($saved -ne "") { [System.Windows.Forms.Clipboard]::SetText($saved) } else { [System.Windows.Forms.Clipboard]::Clear() }
[Console]::Out.Write("OK` + "`n" + `" + $text)
`

func windowsCopyFallback() (text, warning string) {
	out, err := runPowerShell(true, copyScript)
	if err != nil {
		return "", "copy fallback: " + err.Error()
	}
	out = strings.ReplaceAll(out, "\r\n", "\n")
	status, rest, _ := strings.Cut(out, "\n")
	if strings.TrimSpace(status) != "OK" {
		return "", strings.TrimSpace(rest)
	}
	return rest, ""
}

func runPowerShell(sta bool, script string) (string, error) {
	args := []string{"-NoProfile", "-NonInteractive"}
	if sta {
		args = append(args, "-STA")
	}
	// -EncodedCommand (the script as base64 UTF-16) sidesteps every quoting
	// rule between Go, the Windows command line and PowerShell's parser
	args = append(args, "-EncodedCommand", encodeScript(script))
	var out, errb bytes.Buffer
	cmd := exec.Command("powershell", args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			return "", &psError{err, msg}
		}
		return "", err
	}
	return out.String(), nil
}

func encodeScript(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 0, 2*len(u))
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	return base64.StdEncoding.EncodeToString(b)
}

type psError struct {
	err error
	msg string
}

func (e *psError) Error() string { return e.err.Error() + ": " + e.msg }
