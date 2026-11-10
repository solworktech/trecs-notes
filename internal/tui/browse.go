package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/solworktech/trecs-notes/internal/model"
	"github.com/solworktech/trecs-notes/internal/notes"
	"github.com/solworktech/trecs-notes/internal/render"
)

// maxRows is how many matches the list holds; more are said to exist, and
// narrowing the search finds them.
const maxRows = notes.ListCap

// browser is the main page: a search box, the list of matching notes and the
// note that is selected. The searching itself is internal/notes; this is the
// screen and the keys.
type browser struct {
	a *app

	find      *tview.InputField
	matchCase *tview.Checkbox
	regex     *tview.Checkbox
	info      *tview.TextView // how many matches, or why the search cannot run
	list      *tview.Table
	detail    *tview.TextView

	rows   []model.Note // the list; only the screen's goroutine touches it
	capped bool
	// forceMarkdown: nil follows the note, otherwise what F3 chose.
	forceMarkdown *bool

	gen     atomic.Int64 // the search that is current
	listGen int64        // the search the rows belong to
	mu      sync.Mutex
	cancel  context.CancelFunc
	timer   *time.Timer
	keepID  int64 // select this note once the list is built
	wantRow int   // else select this row (after a deletion); -1: neither
	problem string

	shown   string // what the detail pane holds, so a refresh does not jump
	version int64  // the database's data version when the list was last built
}

func newBrowser(a *app) *browser {
	b := &browser{a: a, wantRow: -1}

	field := tcell.ColorDarkSlateGray
	b.find = tview.NewInputField().SetLabel("Find: ").SetFieldBackgroundColor(field).SetFieldTextColor(tcell.ColorWhite)
	b.find.SetLabelColor(tcell.ColorYellow)
	b.find.SetChangedFunc(func(string) { b.schedule() })
	b.find.SetPlaceholder("words, \"a phrase\", tag:x app:y since:2026-01-31 until:...").
		SetPlaceholderStyle(tcell.StyleDefault.Background(field).Foreground(tcell.NewHexColor(0x8a8a8a)))

	toggle := func(label string) *tview.Checkbox {
		c := tview.NewCheckbox().SetLabel(label).SetLabelColor(tcell.ColorWhite)
		c.SetCheckedString("X").SetUncheckedString(" ")
		c.SetFieldBackgroundColor(field).SetFieldTextColor(tcell.ColorWhite)
		c.SetChangedFunc(func(bool) { b.schedule() })
		return c
	}
	b.matchCase = toggle("Match case (Alt+I) ")
	b.regex = toggle("Regex (Alt+R) ")
	b.info = tview.NewTextView().SetDynamicColors(true)

	b.list = tview.NewTable().SetSelectable(true, false).SetFixed(0, 0)
	b.list.SetSelectedStyle(tcell.StyleDefault.Background(tcell.NewHexColor(0x3a3a3a)).Foreground(tcell.ColorWhite))
	b.list.SetSelectionChangedFunc(func(row, _ int) { b.show(row) })
	b.list.SetSelectedFunc(func(row, _ int) { b.a.tv.SetFocus(b.detail) })
	b.detail = tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetScrollable(true)
	b.list.SetBorder(true).SetBorderPadding(0, 0, 1, 1)
	b.detail.SetBorder(true).SetBorderPadding(0, 0, 1, 1)
	b.focusBorders()
	return b
}

// focusBorders marks which of the two panes has the keys.
func (b *browser) focusBorders() {
	set := func(box *tview.Box, on bool) {
		if on {
			box.SetBorderColor(modalMark)
		} else {
			box.SetBorderColor(modalBorder)
		}
	}
	set(b.list.Box, b.list.HasFocus())
	set(b.detail.Box, b.detail.HasFocus())
}

// primitive is the page.
func (b *browser) primitive() tview.Primitive {
	toggles := tview.NewFlex().
		AddItem(b.matchCase, 22, 0, false).AddItem(nil, 2, 0, false).
		AddItem(b.regex, 18, 0, false).AddItem(nil, 2, 0, false).
		AddItem(b.info, 0, 1, false)
	panes := tview.NewFlex().AddItem(b.list, 0, 5, true).AddItem(b.detail, 0, 6, false)
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(b.find, 1, 0, false).
		AddItem(toggles, 1, 0, false).
		AddItem(panes, 0, 1, true)
}

// focusables are what Tab walks through.
func (b *browser) focusables() []tview.Primitive {
	return []tview.Primitive{b.find, b.list, b.detail}
}

func (b *browser) focusNext(step int) {
	all := b.focusables()
	cur := 0
	for i, p := range all {
		if p.HasFocus() {
			cur = i
		}
	}
	b.a.tv.SetFocus(all[(cur+step+len(all))%len(all)])
	b.focusBorders()
}

func (b *browser) typing() bool {
	switch b.a.tv.GetFocus().(type) {
	case *tview.InputField, *tview.TextArea:
		return true
	}
	return false
}

// query is what the search box and the two switches say now.
func (b *browser) query() notes.Query {
	return notes.Query{
		Text:      b.find.GetText(),
		Regex:     b.regex.IsChecked(),
		MatchCase: b.matchCase.IsChecked(),
		Limit:     maxRows + 1,
	}
}

// schedule searches soon: a burst of keystrokes makes one search.
//
// The query is read when the timer fires, on the screen's goroutine: tview
// calls a checkbox's change handler before the box has taken its new state.
func (b *browser) schedule() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(120*time.Millisecond, func() {
		b.a.tv.QueueUpdate(func() { b.start(b.query()) })
	})
}

// searchNow searches at once, for the current query.
func (b *browser) searchNow() { b.start(b.query()) }

// start begins a search, and ends the one still running: its results would
// be for words that are no longer in the box.
func (b *browser) start(q notes.Query) {
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	if b.cancel != nil {
		b.cancel()
	}
	b.cancel = cancel
	b.mu.Unlock()
	gen := b.gen.Add(1)

	go func() {
		defer cancel()
		var batch []model.Note
		last := time.Now()
		count := 0
		capped := false
		flush := func() {
			rows := batch
			batch = nil
			last = time.Now()
			b.a.tv.QueueUpdateDraw(func() { b.append(gen, rows) })
		}
		err := b.a.svc.SearchStream(ctx, q, func(n model.Note) bool {
			count++
			if count > maxRows {
				capped = true
				return false
			}
			batch = append(batch, n)
			if len(batch) >= 100 || (len(batch) > 0 && time.Since(last) > 60*time.Millisecond) {
				flush()
			}
			return true
		})
		if errors.Is(err, context.Canceled) {
			return
		}
		rows := batch
		b.a.tv.QueueUpdateDraw(func() { b.finish(gen, rows, capped, err) })
	}()
}

// reset empties the list for a new search.
func (b *browser) reset() {
	b.rows = nil
	b.list.Clear()
	b.problem = ""
}

func (b *browser) append(gen int64, rows []model.Note) {
	if gen != b.gen.Load() {
		return
	}
	first := b.listGen != gen
	if first {
		b.reset()
		b.listGen = gen
	}
	selected := -1
	for _, n := range rows {
		r := len(b.rows)
		b.rows = append(b.rows, n)
		b.setRow(r, n)
		if b.keepID != 0 && n.ID == b.keepID {
			selected = r
		}
	}
	switch {
	case selected >= 0:
		b.keepID = 0
		b.list.Select(selected, 0)
	case b.wantRow >= 0 && len(b.rows) > b.wantRow:
		b.list.Select(b.wantRow, 0)
		b.wantRow = -1
	case first && b.keepID == 0 && b.wantRow < 0 && len(rows) > 0:
		b.list.Select(0, 0)
		b.list.ScrollToBeginning()
	}
	b.counts(false)
}

func (b *browser) finish(gen int64, rows []model.Note, capped bool, err error) {
	if gen != b.gen.Load() {
		return
	}
	var bad bool
	if errors.Is(err, notes.ErrBadRegex) {
		// the person is still typing: leave what is shown, say why
		bad = true
		b.problem = notes.ProblemText(err)
	} else if err != nil {
		b.problem = err.Error()
		bad = true
	}
	if !bad {
		b.append(gen, rows)
		if b.listGen != gen { // no match at all
			b.reset()
			b.listGen = gen
		}
		b.capped = capped
		switch {
		case b.wantRow >= 0 && len(b.rows) > 0:
			b.list.Select(len(b.rows)-1, 0)
		case b.keepID != 0 && len(b.rows) > 0:
			b.list.Select(0, 0) // the note edited no longer matches
		}
		b.keepID, b.wantRow = 0, -1
	}
	b.counts(true)
	if len(b.rows) == 0 {
		b.show(-1)
	}
}

func (b *browser) counts(done bool) {
	title := fmt.Sprintf(" Notes (%d", len(b.rows))
	if b.capped {
		title += "+"
	}
	title += ") "
	b.list.SetTitle(title)
	switch {
	case b.problem != "":
		b.info.SetText("[" + warn + "]" + tview.Escape(b.problem))
	case !done:
		b.info.SetText("[" + dim + "]Searching...")
	case b.capped:
		b.info.SetText(fmt.Sprintf("[%s]First %d matches; narrow the search to see others.", dim, maxRows))
	case len(b.rows) == 0 && b.find.GetText() != "":
		b.info.SetText("[" + dim + "]No notes match.")
	case len(b.rows) == 0:
		b.info.SetText("[" + dim + "]No notes yet. Select some text anywhere and press the hotkey.")
	default:
		b.info.SetText("")
	}
	if done {
		b.a.refreshStatus()
	}
}

func (b *browser) setRow(r int, n model.Note) {
	cell := func(text string, col tcell.Color) *tview.TableCell {
		return tview.NewTableCell(text).SetTextColor(col).SetSelectable(true)
	}
	mark := " "
	if b.a.svc.Unsent(n) {
		mark = "↑"
	}
	app := n.AppName
	if utf8.RuneCountInString(app) > 14 {
		app = string([]rune(app)[:13]) + "…"
	}
	b.list.SetCell(r, 0, cell(mark, tcell.NewHexColor(0xe0524d)))
	b.list.SetCell(r, 1, cell(n.CreatedAt.Format("2006-01-02 15:04"), tcell.NewHexColor(0x8a8a8a)))
	b.list.SetCell(r, 2, cell(app, tcell.NewHexColor(0x6cb6ff)).SetMaxWidth(14))
	b.list.SetCell(r, 3, cell(notes.Snippet(n.Text), tcell.ColorWhite).SetExpansion(1))
}

// current is the selected note.
func (b *browser) current() (model.Note, bool) {
	row, _ := b.list.GetSelection()
	if row < 0 || row >= len(b.rows) {
		return model.Note{}, false
	}
	return b.rows[row], true
}

// show draws the note at a row of the list in the detail pane.
func (b *browser) show(row int) {
	b.a.clearMessage()
	if row < 0 || row >= len(b.rows) {
		b.shown = ""
		b.detail.SetTitle(" Note ")
		b.detail.SetText("")
		return
	}
	n := b.rows[row]
	var head strings.Builder
	what := n.AppName
	if n.WindowTitle != "" {
		if what != "" {
			what += " - "
		}
		what += n.WindowTitle
	}
	if what == "" {
		what = "(no app or window title)"
	}
	head.WriteString("[::b]" + tview.Escape(what) + "[::-]\n")
	meta := n.CreatedAt.Format("Mon 2 Jan 2006 15:04")
	if len(n.Tags) > 0 {
		meta += "   tags: " + strings.Join(n.Tags, ", ")
	}
	head.WriteString("[" + dim + "]" + tview.Escape(meta) + "\n")
	if text, attention := b.a.svc.SyncState(n); text != "" {
		colour := dim
		if attention {
			colour = warn
		}
		head.WriteString("[" + colour + "]" + tview.Escape(text) + "\n")
	}
	head.WriteString("[-:-:-]\n")

	md := render.LooksLikeMarkdown(n.Text)
	if b.forceMarkdown != nil {
		md = *b.forceMarkdown
	}
	mode := "Raw"
	var body string
	if md {
		mode = "Markdown"
		body = markdownText(render.Markdown(n.Text))
	} else {
		_, spans := render.Auto(n.WindowTitle, n.Text)
		body = rawText(n.Text, spans)
	}
	b.detail.SetTitle(" " + mode + " (F3) ")
	text := head.String() + body
	if text == b.shown {
		return // a refresh found nothing new: do not lose the place
	}
	b.shown = text
	b.detail.SetText(text)
	b.detail.ScrollToBeginning()
}

func (b *browser) toggleMarkdown() {
	n, ok := b.current()
	if !ok {
		return
	}
	md := render.LooksLikeMarkdown(n.Text)
	if b.forceMarkdown != nil {
		md = *b.forceMarkdown
	}
	md = !md
	b.forceMarkdown = &md
	row, _ := b.list.GetSelection()
	b.show(row)
}

func (b *browser) toggleOption(c *tview.Checkbox) { c.SetChecked(!c.IsChecked()) }

// afterChange rebuilds the list once a note has been edited (keep: its id,
// to select again) or removed (keep 0: select what took its place).
func (b *browser) afterChange(keep int64) {
	row, _ := b.list.GetSelection()
	b.keepID, b.wantRow = keep, -1
	if keep == 0 {
		b.wantRow = row
	}
	b.searchNow()
}

// watch notices writes made behind the screen's back - the other frontend
// saving a note, a background sync finishing - and refreshes the list, which
// is cheap, since the search is.
func (b *browser) watch(stop <-chan struct{}) {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		v, err := b.a.svc.DB.DataVersion(context.Background())
		if err != nil {
			continue
		}
		b.a.tv.QueueUpdateDraw(func() {
			if v == b.version {
				return
			}
			b.version = v
			if b.a.modal != "" {
				return
			}
			b.afterRefresh()
		})
	}
}

func (b *browser) afterRefresh() {
	if n, ok := b.current(); ok {
		b.keepID = n.ID
	}
	b.searchNow()
	b.a.refreshStatus()
}
