package notes

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solworktech/trecs-notes/internal/localdb"
	"github.com/solworktech/trecs-notes/internal/model"
)

func TestSnippet(t *testing.T) {
	if got := Snippet("\n  \n\thello\tworld  \nsecond"); got != "hello world" {
		t.Errorf("got %q", got)
	}
	if got := Snippet(strings.Repeat("é", 300)); len([]rune(got)) != 200 {
		t.Errorf("not cut by runes: %d", len([]rune(got)))
	}
	if Snippet(" \n ") != "" {
		t.Error("blank note")
	}
}

func TestProblemText(t *testing.T) {
	if ProblemText(nil) != "" {
		t.Error("nil")
	}
	err := fmt.Errorf("%w: %v", ErrBadRegex, "missing closing )")
	if got := ProblemText(err); got != "not a regular expression yet: missing closing )" {
		t.Errorf("got %q", got)
	}
	if ProblemText(errors.New("disk full")) != "disk full" {
		t.Error("other errors pass through")
	}
}

func TestNoteStatusAndStatusLine(t *testing.T) {
	db, err := localdb.Open(filepath.Join(t.TempDir(), "n.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	s := &Service{DB: db}
	at := time.Date(2026, 11, 9, 21, 8, 0, 0, time.Local)
	n := model.Note{Text: "x", CreatedAt: at.Add(-time.Hour), UpdatedAt: at}

	if got, att := s.NoteStatus(n); got != "last updated at Mon 9 Nov 2026 21:08, remote sync is off." || att {
		t.Errorf("%q %v", got, att)
	}
	if got, _ := s.StatusLine(); got != "0 notes, remote sync is off" {
		t.Errorf("%q", got)
	}

	s.Cfg.RemoteSyncEnabled = true
	for _, c := range []struct {
		n    model.Note
		want string
		att  bool
	}{
		{n, "last updated at Mon 9 Nov 2026 21:08, not on the server yet.", true},
		{model.Note{RemoteID: "r", CreatedAt: n.CreatedAt, UpdatedAt: at}, "last updated at Mon 9 Nov 2026 21:08, synced on server.", false},
		{model.Note{RemoteID: "r", Dirty: true, UpdatedAt: at}, "last updated at Mon 9 Nov 2026 21:08, edited here, not synced yet.", true},
		{model.Note{RemoteSyncError: "no route", CreatedAt: at}, "last updated at Mon 9 Nov 2026 21:08, not synced: no route.", true}, // never edited: the time it was saved
	} {
		if got, att := s.NoteStatus(c.n); got != c.want || att != c.att {
			t.Errorf("%q %v, want %q %v", got, att, c.want, c.att)
		}
	}

	_ = db.Insert(&model.Note{Text: "one"})
	if got, att := s.StatusLine(); got != "1 note, remote sync is on, 1 change(s) not sent yet" || !att {
		t.Errorf("%q %v", got, att)
	}
	id := int64(1)
	_ = db.SetRemoteSync(id, "r1", "")
	_ = db.Insert(&model.Note{Text: "two"})
	_ = db.SetRemoteSync(2, "r2", "")
	if got, att := s.StatusLine(); got != "2 notes, remote sync is on" || att {
		t.Errorf("%q %v", got, att)
	}
}
