package ui

import (
	"fmt"
	"strings"
)

// SecretChoice is the answer to "this note holds secrets".
type SecretChoice int

// The three answers.
const (
	SecretCancel SecretChoice = iota // do not save
	SecretMask                       // save with the secrets masked
	SecretKeep                       // save as it is
)

// maxSecretLines is how many findings the question lists before it says "and
// n more": a note that is a pasted .env file would otherwise make a window
// taller than the screen.
const maxSecretLines = 8

// AskSecrets asks whether a note that holds the described secrets (one line
// each, from notes.DescribeSecrets) is to be saved masked, saved as it is, or
// not saved. It is implemented per-OS like the capture dialog, and it
// returns an error when no dialog could be shown.
func AskSecrets(lines []string) (SecretChoice, error) {
	return askSecrets(secretsMessage(lines))
}

func secretsMessage(lines []string) string {
	var b strings.Builder
	if len(lines) == 1 {
		b.WriteString("This note seems to hold a secret:\n\n")
	} else {
		fmt.Fprintf(&b, "This note seems to hold %d secrets:\n\n", len(lines))
	}
	for i, l := range lines {
		if i == maxSecretLines {
			fmt.Fprintf(&b, "  ...and %d more\n", len(lines)-maxSecretLines)
			break
		}
		b.WriteString("  " + l + "\n")
	}
	b.WriteString("\nMasking replaces each one in the note with [masked: kind] before it is saved.")
	return b.String()
}
