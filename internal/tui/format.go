package tui

import (
	"strings"

	"github.com/rivo/tview"

	"github.com/solworktech/trecs-notes/internal/render"
)

// Hex colours for what internal/render says is a keyword, a string and so
// on: hex, like the other colours here, so that a terminal theme's idea of
// "green" cannot make them unreadable.
var syntaxColour = map[render.Color]string{
	render.Comment: "#6a9955",
	render.String:  "#ce9178",
	render.Number:  "#b5cea8",
	render.Keyword: "#569cd6",
	render.Type:    "#4ec9b0",
	render.Ident:   "#9cdcfe",
	render.Special: "#c586c0",
	render.Preproc: "#c586c0",
	render.Symbol:  "#d4d4d4",
}

const (
	linkColour = "#6cb6ff"
	codeColour = "#e6c07b"
	headColour = "#ffd75f"
)

func tag(fg, attrs string) string {
	if fg == "" {
		fg = "-"
	}
	if attrs == "" {
		attrs = "-"
	}
	return "[" + fg + ":-:" + attrs + "]"
}

// rawText is the note as it was saved, coloured by the spans internal/render
// found in it (offsets count runes).
func rawText(text string, spans []render.Span) string {
	if len(spans) == 0 {
		return tview.Escape(text)
	}
	runes := []rune(text)
	var b strings.Builder
	pos := 0
	for _, s := range spans {
		if s.Start < pos || s.End > len(runes) || s.Start >= s.End {
			continue
		}
		b.WriteString(tview.Escape(string(runes[pos:s.Start])))
		if col := syntaxColour[s.Color]; col != "" {
			b.WriteString(tag(col, "") + tview.Escape(string(runes[s.Start:s.End])) + "[-:-:-]")
		} else {
			b.WriteString(tview.Escape(string(runes[s.Start:s.End])))
		}
		pos = s.End
	}
	b.WriteString(tview.Escape(string(runes[pos:])))
	return b.String()
}

// markdownText is the note as the Markdown preview shows it.
func markdownText(runs []render.Run) string {
	var b strings.Builder
	for _, r := range runs {
		if r.Text == "" {
			continue
		}
		fg, attrs := "", ""
		add := func(a string) {
			if !strings.Contains(attrs, a) {
				attrs += a
			}
		}
		if r.Flags&render.Bold != 0 {
			add("b")
		}
		if r.Flags&render.Italic != 0 {
			add("i")
		}
		if r.Flags&render.Strike != 0 {
			add("s")
		}
		if r.Flags&render.Code != 0 {
			fg = codeColour
		}
		if r.Flags&render.Link != 0 {
			fg = linkColour
			add("u")
		}
		if r.Flags&render.Quote != 0 {
			fg = dim
			add("i")
		}
		if r.Flags&(render.H1|render.H2|render.H3) != 0 {
			fg = headColour
			add("b")
		}
		if col := syntaxColour[r.Color]; col != "" {
			fg = col
		}
		if fg == "" && attrs == "" {
			b.WriteString(tview.Escape(r.Text))
			continue
		}
		b.WriteString(tag(fg, attrs) + tview.Escape(r.Text) + "[-:-:-]")
	}
	return b.String()
}
