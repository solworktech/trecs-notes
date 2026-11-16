package tui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/solworktech/trecs-notes/internal/notes"
	"github.com/solworktech/trecs-notes/internal/secrets"
)

const secretsPage = "secrets"

// The buttons of the question; the first is the one Enter gives.
const (
	btnMask = "Mask and save"
	btnKeep = "Save as is"
	btnBack = "Cancel"
)

// maxSecretLines is how many findings the question lists before "and n more".
const maxSecretLines = 6

func secretsText(found []secrets.Finding) string {
	var b strings.Builder
	if len(found) == 1 {
		b.WriteString("This note seems to hold a secret:\n\n")
	} else {
		fmt.Fprintf(&b, "This note seems to hold %d secrets:\n\n", len(found))
	}
	for i, l := range notes.DescribeSecrets(found) {
		if i == maxSecretLines {
			fmt.Fprintf(&b, "...and %d more\n", len(found)-maxSecretLines)
			break
		}
		b.WriteString(l + "\n")
	}
	b.WriteString("\nMasking replaces each one in the note with [masked: kind].")
	return b.String()
}

// askSecrets puts the question in a modal over the page that is open and
// calls answer with the choice; it returns at once, as the screen is not
// blocked. Escape is a Cancel. Whatever the answer, the page that was open
// has the focus again when answer is called.
func (a *app) askSecrets(found []secrets.Finding, answer func(notes.SecretChoice)) {
	back := a.modal
	modal := tview.NewModal().SetText(secretsText(found)).AddButtons([]string{btnMask, btnKeep, btnBack})
	modal.SetBackgroundColor(modalBackground).SetTextColor(tcell.ColorWhite)
	modal.SetButtonStyle(tcell.StyleDefault.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorWhite))
	modal.SetButtonActivatedStyle(tcell.StyleDefault.Background(modalMark).Foreground(tcell.ColorBlack))
	modal.SetBorderColor(modalBorder)
	modal.SetDoneFunc(func(_ int, label string) {
		a.pages.RemovePage(secretsPage)
		a.modal = back
		switch label {
		case btnMask:
			answer(notes.SecretMask)
		case btnKeep:
			answer(notes.SecretKeep)
		default:
			answer(notes.SecretCancel)
		}
	})
	a.pages.AddPage(secretsPage, modal, true, true)
	a.modal = secretsPage
	a.tv.SetFocus(modal)
}
