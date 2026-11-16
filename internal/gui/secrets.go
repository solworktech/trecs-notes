package gui

import (
	"github.com/solworktech/trecs-notes/internal/notes"
	"github.com/solworktech/trecs-notes/internal/secrets"
	"github.com/solworktech/trecs-notes/internal/ui"
)

// RunSecretsDialog is `trecs-notes secrets-dialog` (internal): the question
// about secrets in a note, in a child process like the other windows.
func RunSecretsDialog() int { return ui.RunSecretsProcess() }

// askSecrets is the notes.SecretAsker of the graphical frontend.
func askSecrets(found []secrets.Finding) (notes.SecretChoice, error) {
	c, err := ui.AskSecrets(notes.DescribeSecrets(found))
	if err != nil {
		return notes.SecretCancel, err
	}
	switch c {
	case ui.SecretMask:
		return notes.SecretMask, nil
	case ui.SecretKeep:
		return notes.SecretKeep, nil
	}
	return notes.SecretCancel, nil
}
