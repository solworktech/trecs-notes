package ui

import (
	"strings"
	"testing"
)

func TestSecretsMessageListsAFewAndCountsTheRest(t *testing.T) {
	one := secretsMessage([]string{"line 2: GitHub token (ghp_…Ux)"})
	if !strings.Contains(one, "hold a secret:") || !strings.Contains(one, "line 2: GitHub token") {
		t.Errorf("one: %q", one)
	}
	var many []string
	for i := 1; i <= 12; i++ {
		many = append(many, "line "+string(rune('0'+i%10))+": x")
	}
	msg := secretsMessage(many)
	if !strings.Contains(msg, "12 secrets") || !strings.Contains(msg, "...and 4 more") || strings.Count(msg, ": x") != maxSecretLines {
		t.Errorf("many: %q", msg)
	}
}
