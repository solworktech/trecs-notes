//go:build windows

package ui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// showCaptureDialog on Windows: a real small multi-field modal, built at
// script time with System.Windows.Forms - the one platform where the
// native scripting layer can give trecs-notes exactly the single form the
// spec describes, labeled fields and all, without a form-building tool
// like yad.
func showCaptureDialog(in Input) (Result, error) {
	hint := ""
	if len(in.TagSuggestions) > 0 {
		hint = " (used before: " + strings.Join(in.TagSuggestions, ", ") + ")"
	}
	script := fmt.Sprintf(formTemplate,
		psQuote(in.Snapshot.AppName),
		psQuote(in.Snapshot.WindowTitle),
		psQuote(hint),
		psQuote(in.Snapshot.SelectedText),
	)
	out, err := runPowerShell(script)
	if err != nil {
		return Result{}, err
	}
	out = strings.TrimRight(out, "\r\n")
	if out == "CANCELLED" {
		return Result{}, nil
	}
	// The form prints four lines: app, window title, tags, then the text
	// body (which may itself contain newlines, so it's always last and
	// everything after the third separator belongs to it).
	parts := strings.SplitN(out, "\x1f", 4)
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

// psQuote renders s as a PowerShell single-quoted string literal (only a
// literal single quote needs doubling inside one).
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// formTemplate builds the modal with New-Object System.Windows.Forms.*
// directly - no Add-Type/P/Invoke needed, since the Forms assembly is
// loaded with a plain -Assembly reference. Fields are pre-filled with the
// %s placeholders (already single-quoted by psQuote above). \x1f (ASCII
// Unit Separator) joins the four output fields on one line, since the text
// field is the only one that may itself contain real newlines.
const formTemplate = `
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$form = New-Object System.Windows.Forms.Form
$form.Text = "Trecs Notes"
$form.Width = 520
$form.Height = 480
$form.StartPosition = "CenterScreen"

function New-Label($text, $y) {
    $l = New-Object System.Windows.Forms.Label
    $l.Text = $text
    $l.Location = New-Object System.Drawing.Point(10, $y)
    $l.AutoSize = $true
    $form.Controls.Add($l)
}

New-Label "App" 10
$appBox = New-Object System.Windows.Forms.TextBox
$appBox.Location = New-Object System.Drawing.Point(10, 30)
$appBox.Width = 480
$appBox.Text = %s
$form.Controls.Add($appBox)

New-Label "Title" 65
$winBox = New-Object System.Windows.Forms.TextBox
$winBox.Location = New-Object System.Drawing.Point(10, 85)
$winBox.Width = 480
$winBox.Text = %s
$form.Controls.Add($winBox)

New-Label ("Tags" + %s) 120
$tagsBox = New-Object System.Windows.Forms.TextBox
$tagsBox.Location = New-Object System.Drawing.Point(10, 140)
$tagsBox.Width = 480
$form.Controls.Add($tagsBox)

New-Label "Note" 175
$textBox = New-Object System.Windows.Forms.TextBox
$textBox.Location = New-Object System.Drawing.Point(10, 195)
$textBox.Width = 480
$textBox.Height = 200
$textBox.Multiline = $true
$textBox.ScrollBars = "Vertical"
$textBox.Text = %s
$form.Controls.Add($textBox)

$okButton = New-Object System.Windows.Forms.Button
$okButton.Text = "Save"
$okButton.Location = New-Object System.Drawing.Point(330, 405)
$okButton.DialogResult = [System.Windows.Forms.DialogResult]::OK
$form.Controls.Add($okButton)
$form.AcceptButton = $okButton

$cancelButton = New-Object System.Windows.Forms.Button
$cancelButton.Text = "Cancel"
$cancelButton.Location = New-Object System.Drawing.Point(415, 405)
$cancelButton.DialogResult = [System.Windows.Forms.DialogResult]::Cancel
$form.Controls.Add($cancelButton)
$form.CancelButton = $cancelButton

$result = $form.ShowDialog()
if ($result -eq [System.Windows.Forms.DialogResult]::OK) {
    $sep = [char]0x1f
    Write-Output ($appBox.Text + $sep + $winBox.Text + $sep + $tagsBox.Text + $sep + $textBox.Text)
} else {
    Write-Output "CANCELLED"
}
`

func runPowerShell(script string) (string, error) {
	tmp, err := os.CreateTemp("", "trecs-notes-*.ps1")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(script); err != nil {
		_ = tmp.Close()
		return "", err
	}
	_ = tmp.Close()

	var out bytes.Buffer
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", tmp.Name())
	cmd.Stdout = &out
	err = runTracked(cmd)
	return out.String(), err
}
