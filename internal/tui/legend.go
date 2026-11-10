package tui

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The legend: the line of keys at the top of a page. It is made of items, and
// is drawn with as many of them as the width of the terminal has room for - the
// most useful first (an item's prio: 1 is kept longest) - and always ends with
// the key that opens the help (preceded by "/ find" on a page that can search),
// which lists everything the page does. The items that are shown keep the order
// they were given in, so the legend does not move about as the window is made
// wider or narrower; the others come back when there is room.
//
// Adapted from trecs-edit (editor/legend.go in github.com/solworktech/trecs).

// legendGap is what separates the items.
const legendGap = "   "

type legendItem struct {
	key, label string
	prio       int // 1 is kept when there is little room
}

func (it legendItem) width() int {
	return utf8.RuneCountInString(it.key) + 1 + utf8.RuneCountInString(it.label)
}

func (it legendItem) tagged() string { return "[yellow]" + it.key + "[white] " + it.label }

// legendFind is the item before the help item on a page that can search.
var legendFind = legendItem{key: "/", label: "find"}

func tailWidth(tail []legendItem) int {
	w := 0
	for i, t := range tail {
		if i > 0 {
			w += len(legendGap)
		}
		w += t.width()
	}
	return w
}

// fitLegendTail chooses the items that fit in width, followed by tail (find,
// then help): all of the tail if it fits, else its last item - the help -
// alone, else none. shown is in the order of items.
func fitLegendTail(items []legendItem, tail []legendItem, width int) (shown []legendItem, kept []legendItem) {
	for len(tail) > 0 && tailWidth(tail) > width {
		tail = tail[1:] // the help is the last, and the last to go
	}
	if len(tail) == 0 {
		return nil, nil
	}
	kept = tail
	avail := width - tailWidth(kept) - len(legendGap)
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return items[order[a]].prio < items[order[b]].prio })
	keep := make([]bool, len(items))
	used, n := 0, 0
	for _, i := range order {
		need := items[i].width()
		if n > 0 {
			need += len(legendGap)
		}
		if used+need > avail {
			continue // this one does not fit; a shorter one that comes after it may
		}
		keep[i] = true
		used += need
		n++
	}
	for i, it := range items {
		if keep[i] {
			shown = append(shown, it)
		}
	}
	return shown, kept
}

// legendTextTail is the legend as tagged text, for the width.
func legendTextTail(items []legendItem, tail []legendItem, width int) string {
	shown, kept := fitLegendTail(items, tail, width)
	parts := make([]string, 0, len(shown)+len(kept))
	for _, it := range shown {
		parts = append(parts, it.tagged())
	}
	for _, it := range kept {
		parts = append(parts, it.tagged())
	}
	return strings.Join(parts, legendGap)
}

// legendBar is the one line a page shows its legend in.
type legendBar struct {
	*tview.Box
	items   []legendItem
	helpKey string
	find    bool // the page can search: "/ find" comes before the help
}

func newLegendBar(helpKey string, items ...legendItem) *legendBar {
	return &legendBar{Box: tview.NewBox(), items: items, helpKey: helpKey}
}

func (l *legendBar) tail() []legendItem {
	t := []legendItem{{key: l.helpKey, label: "help"}}
	if l.find {
		t = append([]legendItem{legendFind}, t...)
	}
	return t
}

func (l *legendBar) Draw(screen tcell.Screen) {
	l.DrawForSubclass(screen, l)
	x, y, w, h := l.GetInnerRect()
	if h < 1 || w < 1 {
		return
	}
	tview.Print(screen, legendTextTail(l.items, l.tail(), w), x, y, w, tview.AlignLeft, tcell.ColorWhite)
}
