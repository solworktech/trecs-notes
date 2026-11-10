package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// What the modals look like, as in trecs-edit: the dark of a field is the dark
// of the whole modal, and what is marked is a green.
var (
	modalBackground = tcell.ColorBlack
	modalMark       = tcell.NewRGBColor(76, 175, 80)
	modalBorder     = tcell.NewRGBColor(110, 118, 129)
)

// The colours of text are given as hex, and not as names: a name is the
// terminal's own idea of "gray" or "red", which some themes make too dark to
// read.
const (
	dim  = "#8a8a8a"
	warn = "#e0524d"
)

// opaque makes a flex paint its background before it draws what it holds,
// which tview's flex does not (it never clears its area): what is under it
// would show in the space between and around what it holds. The draw function
// runs after the border is drawn and before the items are, so it paints
// everything inside the border. (From trecs-edit.)
func opaque(f *tview.Flex, bordered bool) *tview.Flex {
	f.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		bx, by, bw, bh := x, y, width, height
		if bordered {
			bx, by, bw, bh = x+1, y+1, width-2, height-2
		}
		style := tcell.StyleDefault.Background(f.GetBackgroundColor())
		for yy := by; yy < by+bh; yy++ {
			for xx := bx; xx < bx+bw; xx++ {
				screen.SetContent(xx, yy, ' ', nil, style)
			}
		}
		return f.GetInnerRect()
	})
	return f
}

// centred puts a box of the size in the middle of the screen.
func centred(box tview.Primitive, width, height int) tview.Primitive {
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().AddItem(nil, 0, 1, false).AddItem(box, width, 0, true).AddItem(nil, 0, 1, false), height, 0, true).
		AddItem(nil, 0, 1, false)
}

type helpRow struct{ key, text string }

// newHelp is the help modal: a list of keys and what they do.
func newHelp(title string, rows []helpRow) (frame tview.Primitive, text *tview.TextView) {
	width := 0
	for _, r := range rows {
		width = max(width, utf8.RuneCountInString(r.key))
	}
	var sb strings.Builder
	for _, r := range rows {
		sb.WriteString("[yellow]" + tview.Escape(r.key) + strings.Repeat(" ", width-utf8.RuneCountInString(r.key)) + "[white]  " + tview.Escape(r.text) + "\n")
	}
	sb.WriteString("\n[" + dim + "]Esc closes this")
	text = tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetText(sb.String())
	text.SetBackgroundColor(modalBackground)
	box := opaque(tview.NewFlex(), true).SetDirection(tview.FlexRow).AddItem(text, 0, 1, true)
	box.SetBorder(true).SetTitle(" "+title+" - keys ").SetBorderPadding(0, 0, 1, 1)
	box.SetBackgroundColor(modalBackground)
	return centred(box, 84, len(rows)+4), text
}
