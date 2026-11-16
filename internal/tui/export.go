package tui

import (
	"context"
	"errors"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/solworktech/trecs-notes/internal/notes"
)

const (
	exportPage = "export"
	lblFile    = "File"
)

// exportModal asks where to write the notes the list shows, as CSV.
type exportModal struct {
	a       *app
	frame   tview.Primitive
	form    *tview.Form
	message *tview.TextView
	// replace is the path that the person was told exists and asked for
	// again: asking twice for the same file is the "yes, replace it".
	replace string
	busy    bool
}

func newExportModal(a *app) *exportModal {
	m := &exportModal{a: a}
	m.form = tview.NewForm()
	m.form.SetBackgroundColor(modalBackground).SetBorderPadding(0, 0, 0, 0)
	m.form.SetFieldBackgroundColor(tcell.ColorDarkSlateGray).SetFieldTextColor(tcell.ColorWhite)
	m.form.SetLabelColor(tcell.ColorWhite)
	m.form.SetButtonStyle(tcell.StyleDefault.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorWhite))
	m.form.SetButtonActivatedStyle(tcell.StyleDefault.Background(modalMark).Foreground(tcell.ColorBlack))
	m.form.SetButtonsAlign(tview.AlignLeft).SetItemPadding(1)

	m.message = tview.NewTextView().SetDynamicColors(true)
	m.message.SetBackgroundColor(modalBackground)

	box := opaque(tview.NewFlex(), true).SetDirection(tview.FlexRow).
		AddItem(m.form, 0, 1, true).
		AddItem(m.message, 2, 0, false)
	box.SetBorder(true).SetBorderColor(modalBorder).SetTitle(" Export the notes shown (CSV) ").SetBorderPadding(0, 0, 1, 1)
	box.SetBackgroundColor(modalBackground)

	m.frame = centred(box, 90, 8)
	m.frame.(*tview.Flex).SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEscape {
			m.close()
			return nil
		}
		return ev
	})
	return m
}

func (m *exportModal) open() {
	path, err := notes.DefaultExportPath(time.Now())
	if err != nil {
		path = notes.ExportName(time.Now())
	}
	m.replace, m.busy = "", false
	m.form.Clear(true)
	m.form.AddInputField(lblFile, path, 0, nil, func(string) { m.replace = "" })
	m.form.AddButton("Export", m.run)
	m.form.AddButton("Cancel Esc", m.close)
	m.message.SetText("[" + dim + "]A folder gets the default file name; ~ is your home folder.")
	m.a.pages.AddPage(exportPage, m.frame, true, true)
	m.a.modal = exportPage
	m.a.tv.SetFocus(m.form)
}

func (m *exportModal) close() {
	m.a.pages.RemovePage(exportPage)
	m.a.modal = ""
	m.a.focusList()
}

func (m *exportModal) fail(msg string) {
	m.message.SetText("[" + warn + "]" + tview.Escape(msg))
	m.a.tv.SetFocus(m.form)
}

// run exports the notes the list shows now: the same search, without the
// list's cap. It does not block the screen.
func (m *exportModal) run() {
	if m.busy {
		return
	}
	input := m.form.GetFormItemByLabel(lblFile).(*tview.InputField).GetText()
	path, err := notes.ResolveExportPath(input, time.Now())
	if err != nil {
		m.fail(err.Error())
		return
	}
	q := m.a.browse.query()
	overwrite := path == m.replace
	m.busy = true
	m.message.SetText("[" + dim + "]Exporting...")
	go func() {
		n, err := m.a.svc.ExportCSV(context.Background(), q, path, overwrite)
		m.a.tv.QueueUpdateDraw(func() {
			m.busy = false
			if m.a.modal != exportPage { // closed with Esc while it ran
				if err == nil {
					m.a.say(notes.ExportMessage(n, path))
				}
				return
			}
			switch {
			case errors.Is(err, notes.ErrExportExists):
				m.replace = path
				m.fail(path + " exists. Export again to replace it, or change the name.")
			case errors.Is(err, notes.ErrNothingToExport):
				m.fail("There are no notes shown, so there is nothing to export.")
			case err != nil:
				m.fail(notes.ProblemText(err))
			default:
				m.close()
				m.a.say(notes.ExportMessage(n, path))
			}
		})
	}()
}
