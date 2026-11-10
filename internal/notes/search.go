package notes

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/solworktech/trecs-notes/internal/localdb"
	"github.com/solworktech/trecs-notes/internal/model"
)

// Query is what a person typed into a search box, and the two switches next
// to it. Both frontends build one of these and call Search; neither knows
// how a search is carried out.
//
// The text is a list of words (all must appear, whole words; the last one
// may be a beginning, so results appear as you type) or "quoted phrases",
// mixed with filters:
//
//	tag:k8s      has this tag (repeatable)
//	app:firefox  the app's name contains this
//	since:2026-01-31   saved on or after this day
//	until:2026-02-28   saved on or before this day
//
// With Regex on, whatever is left once the filters are taken out is a
// regular expression, tried against the text, app name and window title.
type Query struct {
	Text string
	// Regex treats the text as a regular expression instead of words.
	Regex bool
	// MatchCase makes the match case-sensitive. With it off, case counts
	// only if the text has a capital letter (as in trecs-edit).
	MatchCase bool
	// Limit stops after this many notes (0: all of them).
	Limit int
}

// parsed is a Query taken apart.
type parsed struct {
	words   []string // raw tokens, quotes kept
	filter  localdb.Filter
	pattern string // the text with the filters removed
}

func tokenize(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case unicode.IsSpace(r) && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

func parseDay(s string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", s, time.Local)
}

func parseQuery(q Query) (parsed, error) {
	var p parsed
	var rest []string
	for _, tok := range tokenize(q.Text) {
		key, val, ok := strings.Cut(tok, ":")
		if ok && val != "" {
			switch strings.ToLower(key) {
			case "tag":
				p.filter.Tags = append(p.filter.Tags, strings.ToLower(val))
				continue
			case "app":
				p.filter.App = strings.Trim(val, `"`)
				continue
			case "since", "after":
				d, err := parseDay(val)
				if err != nil {
					return p, fmt.Errorf("%s: want a date like 2026-01-31", key)
				}
				p.filter.Since = d.Unix()
				continue
			case "until", "before":
				d, err := parseDay(val)
				if err != nil {
					return p, fmt.Errorf("%s: want a date like 2026-01-31", key)
				}
				p.filter.Until = d.AddDate(0, 0, 1).Unix()
				continue
			}
		}
		rest = append(rest, tok)
	}
	p.words = rest
	p.pattern = strings.Join(rest, " ")
	return p, nil
}

func hasLetterOrDigit(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// ftsExpression turns words into an FTS5 expression: every word must
// appear; a "phrase" must appear in that order; the last bare word may be
// the start of a longer one.
func ftsExpression(words []string) (expr string, needles []string) {
	var parts []string
	for i, w := range words {
		phrase := len(w) >= 2 && strings.HasPrefix(w, `"`) && strings.HasSuffix(w, `"`)
		w = strings.Trim(w, `"`)
		if !hasLetterOrDigit(w) {
			continue
		}
		needles = append(needles, w)
		part := `"` + strings.ReplaceAll(w, `"`, `""`) + `"`
		if !phrase && i == len(words)-1 {
			part += "*"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " AND "), needles
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func haystack(n model.Note) string {
	return n.Text + "\n" + n.AppName + "\n" + n.WindowTitle
}

// ErrBadRegex wraps a regular expression that does not compile, so a
// frontend can show it quietly while the person is still typing.
var ErrBadRegex = errors.New("invalid regular expression")

// Search finds the notes a query describes, newest first.
func (s *Service) Search(ctx context.Context, q Query) ([]model.Note, error) {
	var out []model.Note
	err := s.SearchStream(ctx, q, func(n model.Note) bool {
		out = append(out, n)
		return true
	})
	return out, err
}

// SearchStream calls emit with each match as it is found, newest first,
// until emit returns false, the limit is reached or ctx is cancelled (which
// ends it with ctx's error). A frontend running it in a goroutine can show
// the first page while the rest is still being looked for, and drop a
// search that the next keystroke has made pointless.
func (s *Service) SearchStream(ctx context.Context, q Query, emit func(model.Note) bool) error {
	p, err := parseQuery(q)
	if err != nil {
		return err
	}
	count := 0
	out := func(n model.Note) bool {
		count++
		if !emit(n) {
			return false
		}
		return q.Limit <= 0 || count < q.Limit
	}

	if strings.TrimSpace(p.pattern) == "" {
		return s.DB.Scan(ctx, p.filter, out)
	}

	if !q.Regex {
		expr, needles := ftsExpression(p.words)
		if expr == "" {
			return s.DB.Scan(ctx, p.filter, out)
		}
		f := p.filter
		f.Match = expr
		sensitive := q.MatchCase || hasUpper(p.pattern)
		return s.DB.Scan(ctx, f, func(n model.Note) bool {
			if sensitive {
				h := haystack(n)
				for _, w := range needles {
					if !strings.Contains(h, w) {
						return true
					}
				}
			}
			return out(n)
		})
	}

	pat := p.pattern
	if !q.MatchCase && !hasUpper(pat) {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile("(?m)" + pat)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadRegex, err)
	}
	if corpus, ok := s.corpusFor(ctx); ok {
		for i, n := range corpus {
			if i%256 == 0 && ctx.Err() != nil {
				return ctx.Err()
			}
			if matchesFilter(n, p.filter) && re.MatchString(haystack(n)) && !out(n) {
				return nil
			}
		}
		return ctx.Err()
	}
	return s.DB.Scan(ctx, p.filter, func(n model.Note) bool {
		if re.MatchString(haystack(n)) {
			return out(n)
		}
		return true
	})
}

// matchesFilter is localdb's Filter (minus Match) applied in memory.
func matchesFilter(n model.Note, f localdb.Filter) bool {
	if f.Since > 0 && n.CreatedAt.Unix() < f.Since {
		return false
	}
	if f.Until > 0 && n.CreatedAt.Unix() >= f.Until {
		return false
	}
	if f.App != "" && !strings.Contains(strings.ToLower(n.AppName), strings.ToLower(f.App)) {
		return false
	}
	for _, want := range f.Tags {
		found := false
		for _, t := range n.Tags {
			if t == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// corpusMaxBytes caps what the in-memory copy of the notes may hold; past it
// regex searches go back to reading the database as they run.
const corpusMaxBytes = 128 << 20

// corpus is every note in memory, for regular-expression searches, which
// the full-word index cannot help with. It is rebuilt when the database's
// data version moves (any write, by this process or another).
type corpus struct {
	mu      sync.Mutex
	version int64
	valid   bool
	tooBig  bool
	notes   []model.Note
}

// corpusFor returns the cached notes, loading them first if the database has
// changed. false means: too large, or could not be loaded; scan instead.
func (s *Service) corpusFor(ctx context.Context) ([]model.Note, bool) {
	ver, err := s.DB.DataVersion(ctx)
	if err != nil {
		return nil, false
	}
	c := &s.cache
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid && c.version == ver {
		return c.notes, !c.tooBig
	}
	c.valid, c.tooBig, c.notes = false, false, nil
	var all []model.Note
	size := 0
	err = s.DB.Scan(ctx, localdb.Filter{}, func(n model.Note) bool {
		size += len(n.Text) + len(n.AppName) + len(n.WindowTitle) + 64
		if size > corpusMaxBytes {
			c.tooBig = true
			return false
		}
		all = append(all, n)
		return true
	})
	if err != nil {
		return nil, false
	}
	c.version, c.valid = ver, true
	if c.tooBig {
		return nil, false
	}
	c.notes = all
	return all, true
}
