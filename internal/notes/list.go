package notes

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/solworktech/trecs-notes/internal/model"
)

// ListCap is how many matches a list shows; more are said to exist, and
// narrowing the search finds them. Both frontends use it.
const ListCap = 2000

// Snippet is what stands for a note in a list: its first line that is not
// blank, tidied and cut to a length a list can clip.
func Snippet(text string) string {
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			l = strings.ReplaceAll(l, "\t", " ")
			if utf8.RuneCountInString(l) > 200 {
				l = string([]rune(l)[:200])
			}
			return l
		}
	}
	return ""
}

// ProblemText is how a failed search is put to a person: a regular
// expression that does not compile yet is no error while it is being typed.
func ProblemText(err error) string {
	if err == nil {
		return ""
	}
	if strings.Contains(err.Error(), ErrBadRegex.Error()) {
		return "not a regular expression yet: " + strings.TrimPrefix(err.Error(), ErrBadRegex.Error()+": ")
	}
	return err.Error()
}

// NoteStatus is the status line of one note: when it was last updated and
// where it stands with the server ("last updated at Mon 9 Nov 2026 21:08,
// synced on server."). attention is true when it needs the person's notice.
func (s *Service) NoteStatus(n model.Note) (text string, attention bool) {
	at := n.UpdatedAt
	if at.IsZero() {
		at = n.CreatedAt
	}
	text = "last updated at " + at.Format("Mon 2 Jan 2006 15:04") + ", "
	switch {
	case !s.Cfg.RemoteSyncEnabled:
		return text + "remote sync is off.", false
	case n.RemoteSyncError != "":
		return text + "not synced: " + n.RemoteSyncError + ".", true
	case n.RemoteID == "":
		return text + "not on the server yet.", true
	case n.Dirty:
		return text + "edited here, not synced yet.", true
	}
	return text + "synced on server.", false
}

// Unsent reports whether a list should mark the note as not (fully) sent.
func (s *Service) Unsent(n model.Note) bool {
	return s.Cfg.RemoteSyncEnabled && (n.RemoteID == "" || n.Dirty)
}

// StatusLine is the sentence at the bottom of a window: how many notes there
// are and where syncing stands. attention is true when changes are waiting.
func (s *Service) StatusLine() (text string, attention bool) {
	total, _ := s.DB.Count()
	text = fmt.Sprintf("%d notes", total)
	if total == 1 {
		text = "1 note"
	}
	if !s.Cfg.RemoteSyncEnabled {
		return text + ", remote sync is off", false
	}
	if n := s.PendingChanges(); n > 0 {
		return text + fmt.Sprintf(", remote sync is on, %d change(s) not sent yet", n), true
	}
	return text + ", remote sync is on", false
}
