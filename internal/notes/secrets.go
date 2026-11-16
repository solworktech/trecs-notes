package notes

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/solworktech/trecs-notes/internal/secrets"
)

// SecretChoice is what the person decided when told a note holds secrets.
type SecretChoice int

// The answers to "this note holds a secret".
const (
	SecretCancel SecretChoice = iota // do not save
	SecretMask                       // save with the secrets replaced
	SecretKeep                       // save as it is
)

// SecretAsker puts the findings in front of the person and returns their
// answer; each frontend has its own (a window, a terminal modal). An error
// means the question could not be asked.
type SecretAsker func(findings []secrets.Finding) (SecretChoice, error)

// FindSecrets is what in text looks like a credential. A secret that is also
// in previous (the text as it was stored) is not reported: the person has
// already seen it and kept it, and asking again on every edit of the tags
// would only teach them to click through. It is empty when the check is off.
func (s *Service) FindSecrets(text, previous string) []secrets.Finding {
	if !s.Cfg.SecretCheck {
		return nil
	}
	var out []secrets.Finding
	for _, f := range secrets.Default().Scan(text) {
		if previous != "" && strings.Contains(previous, f.Secret) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// ErrDeclined is returned, and nothing is saved, when the person answered
// "cancel" to the question about secrets.
var ErrDeclined = errors.New("not saved: the note holds secrets and you chose not to save it")

// CheckSecrets decides what to store for text. When it holds secrets the
// person is asked (see SecretAsker); the answer gives the text to store,
// masked or not, and how many secrets were masked in it. "Cancel" is
// ErrDeclined. A question that cannot be asked (no display, no dialog tool)
// is answered with "mask": the note is saved, and the secret is not kept.
func (s *Service) CheckSecrets(text, previous string, ask SecretAsker) (store string, masked int, err error) {
	found := s.FindSecrets(text, previous)
	if len(found) == 0 {
		return text, 0, nil
	}
	choice, err := ask(found)
	if err != nil {
		log.Printf("secrets: the note holds %s and the question about it could not be asked (%v); masked them", plural(len(found), "secret"), err)
		return MaskSecrets(text, found), len(found), nil
	}
	switch choice {
	case SecretMask:
		return MaskSecrets(text, found), len(found), nil
	case SecretKeep:
		return text, 0, nil
	}
	return "", 0, ErrDeclined
}

// MaskSecrets replaces each secret in text with "[masked: rule-id]".
func MaskSecrets(text string, found []secrets.Finding) string {
	fs := append([]secrets.Finding(nil), found...)
	// longest first: a secret that is part of a longer one must not leave
	// the rest of the longer one behind
	sort.SliceStable(fs, func(i, j int) bool { return len(fs[i].Secret) > len(fs[j].Secret) })
	for _, f := range fs {
		if f.Secret != "" {
			text = strings.ReplaceAll(text, f.Secret, "[masked: "+f.RuleID+"]")
		}
	}
	return text
}

// DescribeSecrets is one line for each finding, for the question: where it
// is, what it is, and enough of it to recognise and not to use.
func DescribeSecrets(found []secrets.Finding) []string {
	lines := make([]string, 0, len(found))
	for _, f := range found {
		what := f.Description
		if what == "" {
			what = f.RuleID
		}
		lines = append(lines, fmt.Sprintf("line %d: %s (%s)", f.Line, what, secrets.Mask(f.Secret)))
	}
	return lines
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// SecretsMasked is the sentence for a frontend to say after a note was
// saved with its secrets masked.
func SecretsMasked(n int) string { return "Note saved; " + plural(n, "secret") + " masked." }
