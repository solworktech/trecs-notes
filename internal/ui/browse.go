package ui

import "context"

// BrowseRow is one line of the browse window's list.
type BrowseRow struct {
	ID      int64
	Mark    string // "↑" when the server has not seen the note's latest text
	Date    string
	App     string
	Snippet string
}

// BrowseNote is the note shown on the right of the browse window, and what
// comes back from it when saved.
type BrowseNote struct {
	ID       int64
	App      string
	Title    string
	Tags     string // as typed: separated by commas or spaces
	Text     string
	Created  string // "Mon 2 Jan 2006 15:04"
	Sync     string // where the note stands with the server; "" when syncing is off
	Markdown bool   // the note looks like Markdown: open on that tab
}

// BrowseHandler is what the browse window calls. It does the work (internal/gui
// supplies it, from internal/notes); the window only draws. Search runs on its
// own goroutine, and stops when ctx is cancelled (the next keystroke).
type BrowseHandler struct {
	// Search streams the matching rows to emit (which returns false to
	// stop). capped: there were more than the list holds; problem: why the
	// search cannot run (shown, not fatal).
	Search func(ctx context.Context, text string, regex, matchCase bool, emit func(BrowseRow) bool) (capped bool, problem string)
	Get    func(id int64) (BrowseNote, error)
	Save   func(n BrowseNote) (message string, err error)
	Delete func(id int64) error
	// Sync sends what the server has not seen.
	Sync func() (message string, ok bool)
	// Status is the line at the bottom; attention: changes are waiting.
	Status func() (text string, attention bool)
	// Changed reports whether the database changed since the last call.
	Changed func() bool
	// Tags lists the tags in use, for completion.
	Tags func() []string
}

// BrowseSize is the window's size (0: the default).
type BrowseSize struct{ Width, Height int }
