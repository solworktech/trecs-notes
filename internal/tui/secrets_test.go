package tui

import (
	"strings"
	"testing"

	"github.com/solworktech/trecs-notes/internal/secrets"
)

func TestSecretsTextNamesTheKindAndHidesTheSecret(t *testing.T) {
	tok := "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"
	got := secretsText(secrets.Default().Scan("key " + tok))
	if !strings.Contains(got, "hold a secret:") || !strings.Contains(got, "line 1: ") || strings.Contains(got, tok) {
		t.Errorf("got %q", got)
	}
}

func TestSecretsTextCountsWhatItDoesNotList(t *testing.T) {
	var found []secrets.Finding
	for i := 0; i < maxSecretLines+3; i++ {
		found = append(found, secrets.Finding{RuleID: "x", Description: "thing", Line: i + 1, Secret: "abcdefghijklmnop"})
	}
	got := secretsText(found)
	if !strings.Contains(got, "9 secrets") || !strings.Contains(got, "...and 3 more") {
		t.Errorf("got %q", got)
	}
}
