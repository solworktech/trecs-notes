package notes

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solworktech/trecs-notes/internal/config"
	"github.com/solworktech/trecs-notes/internal/localdb"
	"github.com/solworktech/trecs-notes/internal/model"
	"github.com/solworktech/trecs-notes/internal/secrets"
)

const (
	tokA = "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"
	tokB = "xoxb-123456789012-1234567890123-AbCdEfGhIjKlMnOpQrStUvWx"
)

func svcOn(on bool) *Service { return &Service{Cfg: config.Config{SecretCheck: on}} }

func answer(c SecretChoice, calls *int) SecretAsker {
	return func([]secrets.Finding) (SecretChoice, error) { *calls++; return c, nil }
}

func TestAPlainNoteIsNotQuestioned(t *testing.T) {
	var calls int
	out, n, err := svcOn(true).CheckSecrets("buy milk, call Sam", "", answer(SecretCancel, &calls))
	if out != "buy milk, call Sam" || n != 0 || err != nil || calls != 0 {
		t.Fatalf("got %q %d %v, asked %d times", out, n, err, calls)
	}
}

func TestMaskReplacesEverySecretAndOnlyThose(t *testing.T) {
	var calls int
	text := "token " + tokA + "\nagain " + tokA + "\nslack " + tokB + "\nand some prose"
	out, n, err := svcOn(true).CheckSecrets(text, "", answer(SecretMask, &calls))
	if err != nil || n != 2 || calls != 1 {
		t.Fatal(out, n, err, calls)
	}
	if strings.Contains(out, tokA) || strings.Contains(out, tokB) {
		t.Errorf("still there: %q", out)
	}
	if !strings.Contains(out, "[masked: github-pat]") || !strings.Contains(out, "[masked: slack-bot-token]") || !strings.Contains(out, "and some prose") {
		t.Errorf("got %q", out)
	}
	if strings.Count(out, "[masked: github-pat]") != 2 {
		t.Errorf("both copies of the token should go: %q", out)
	}
}

func TestKeepStoresTheTextAsItIsAndCancelStoresNothing(t *testing.T) {
	var calls int
	text := "export GITHUB_TOKEN=" + tokA
	out, n, err := svcOn(true).CheckSecrets(text, "", answer(SecretKeep, &calls))
	if out != text || n != 0 || err != nil {
		t.Errorf("keep: %q %d %v", out, n, err)
	}
	out, _, err = svcOn(true).CheckSecrets(text, "", answer(SecretCancel, &calls))
	if out != "" || !errors.Is(err, ErrDeclined) {
		t.Errorf("cancel: %q %v", out, err)
	}
}

func TestAQuestionThatCannotBeAskedMasksAndSaves(t *testing.T) {
	ask := func([]secrets.Finding) (SecretChoice, error) { return SecretKeep, errors.New("no display") }
	out, n, err := svcOn(true).CheckSecrets("key "+tokA, "", ask)
	if err != nil || n != 1 || strings.Contains(out, tokA) || !strings.Contains(out, "[masked: github-pat]") {
		t.Errorf("got %q %d %v", out, n, err)
	}
}

func TestTheSwitchTurnsItOff(t *testing.T) {
	var calls int
	text := "key " + tokA
	out, _, err := svcOn(false).CheckSecrets(text, "", answer(SecretCancel, &calls))
	if out != text || err != nil || calls != 0 {
		t.Errorf("got %q %v, asked %d", out, err, calls)
	}
}

func TestASecretAlreadyInTheStoredNoteIsNotAskedAboutAgain(t *testing.T) {
	var calls int
	old := "key " + tokA
	// an edit that adds prose: the token was kept before, so no question
	if _, _, err := svcOn(true).CheckSecrets(old+"\nmore", old, answer(SecretCancel, &calls)); err != nil || calls != 0 {
		t.Errorf("asked %d times, err=%v", calls, err)
	}
	// but a new one is asked about, and only it is reported
	var seen []secrets.Finding
	ask := func(f []secrets.Finding) (SecretChoice, error) { seen = f; return SecretMask, nil }
	out, _, _ := svcOn(true).CheckSecrets(old+"\n"+tokB, old, ask)
	if len(seen) != 1 || seen[0].RuleID != "slack-bot-token" {
		t.Errorf("seen %+v", seen)
	}
	if !strings.Contains(out, tokA) || strings.Contains(out, tokB) {
		t.Errorf("the old one stays, the new one goes: %q", out)
	}
}

func TestDescribeNeverShowsTheWholeSecret(t *testing.T) {
	f := secrets.Default().Scan("key " + tokA)
	lines := DescribeSecrets(f)
	if len(lines) != 1 || strings.Contains(lines[0], tokA) || !strings.HasPrefix(lines[0], "line 1: ") {
		t.Errorf("got %q", lines)
	}
}

func TestSaveAndUpdateCheckedStoreWhatWasChosen(t *testing.T) {
	db, err := localdb.Open(filepath.Join(t.TempDir(), "n.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	s := &Service{DB: db, Cfg: config.Config{SecretCheck: true}}
	var calls int

	// declined: nothing is stored
	if _, err := s.SaveChecked(model.Note{AppName: "sh", Text: "key " + tokA}, answer(SecretCancel, &calls)); !errors.Is(err, ErrDeclined) {
		t.Fatalf("got %v", err)
	}
	if c, _ := db.Count(); c != 0 {
		t.Fatalf("%d notes stored after a cancel", c)
	}

	// masked
	n, err := s.SaveChecked(model.Note{AppName: "sh", Text: "key " + tokA}, answer(SecretMask, &calls))
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	note, err := db.Get(1)
	if err != nil || strings.Contains(note.Text, tokA) || !strings.Contains(note.Text, "[masked: github-pat]") {
		t.Fatalf("stored %+v, %v", note, err)
	}

	// an edit that brings in a new secret is asked about
	note.Text += " and " + tokB
	var seen int
	ask := func(f []secrets.Finding) (SecretChoice, error) { seen = len(f); return SecretKeep, nil }
	if _, err := s.UpdateChecked(note, ask); err != nil || seen != 1 {
		t.Fatal(seen, err)
	}
}
