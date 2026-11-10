package tui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/solworktech/trecs-notes/internal/model"
	"github.com/solworktech/trecs-notes/internal/notes"
)

const (
	editPage    = "edit"
	confirmPage = "confirm"

	lblApp   = "App"
	lblTitle = "Window title"
	lblTags  = "Tags"
	lblText  = "Text"
)

// editModal changes a note's app, window title, tags and text.
type editModal struct {
	a       *app
	frame   tview.Primitive
	form    *tview.Form
	message *tview.TextView
	note    model.Note
}

func newEditModal(a *app) *editModal {
	m := &editModal{a: a}
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
		AddItem(m.message, 1, 0, false)
	box.SetBorder(true).SetBorderColor(modalBorder).SetTitle(" Edit note ").SetBorderPadding(0, 0, 1, 1)
	box.SetBackgroundColor(modalBackground)

	m.frame = centred(box, 80, 24)
	m.frame.(*tview.Flex).SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			m.close()
			return nil
		case tcell.KeyCtrlS:
			m.save()
			return nil
		case tcell.KeyCtrlE:
			// in the text box, ^E opens the text in the person's editor
			// (elsewhere it does nothing; the box's own ^E, "end of line",
			// is given up for it)
			if item, _ := m.form.GetFocusedItemIndex(); item == 3 {
				m.externalEdit()
			}
			return nil
		}
		return ev
	})
	return m
}

func (m *editModal) open(n model.Note) {
	m.note = n
	m.form.Clear(true)
	m.form.AddInputField(lblApp, n.AppName, 0, nil, nil)
	m.form.AddInputField(lblTitle, n.WindowTitle, 0, nil, nil)
	m.form.AddInputField(lblTags, strings.Join(n.Tags, ", "), 0, nil, nil)
	m.form.AddTextArea(lblText, n.Text, 0, 11, 0, nil)
	m.form.AddButton("Save ^S", m.save)
	m.form.AddButton("Cancel Esc", m.close)
	m.form.SetFocus(3) // the text is what is usually changed
	m.message.SetText("")
	m.a.pages.AddPage(editPage, m.frame, true, true)
	m.a.modal = editPage
	m.focus()
}

func (m *editModal) focus() { m.a.tv.SetFocus(m.form) }

func (m *editModal) close() {
	m.a.pages.RemovePage(editPage)
	m.a.modal = ""
	m.a.focusList()
}

func (m *editModal) input(label string) string {
	return m.form.GetFormItemByLabel(label).(*tview.InputField).GetText()
}

// externalEdit hands the text to $VISUAL or $EDITOR: the screen is given up
// while it runs and redrawn after, with the text it left.
func (m *editModal) externalEdit() {
	ta := m.form.GetFormItemByLabel(lblText).(*tview.TextArea)
	var out string
	var err error
	editor := editorCommand()
	m.a.tv.Suspend(func() { out, err = editExternally(editor, ta.GetText()) })
	if err != nil {
		m.message.SetText("[" + warn + "]" + tview.Escape(err.Error()))
		return
	}
	ta.SetText(out, true)
	m.message.SetText("[" + dim + "]Text taken from " + tview.Escape(editor) + ". ^S saves the note.")
}

func (m *editModal) save() {
	n := m.note
	n.AppName = strings.TrimSpace(m.input(lblApp))
	n.WindowTitle = strings.TrimSpace(m.input(lblTitle))
	n.Tags = model.SplitTagInput(m.input(lblTags))
	n.Text = m.form.GetFormItemByLabel(lblText).(*tview.TextArea).GetText()
	if err := m.a.svc.Update(n); err != nil {
		msg := err.Error()
		if err == notes.ErrEmptyNote {
			msg = "A note needs some text."
		}
		m.message.SetText("[" + warn + "]" + tview.Escape(msg))
		return
	}
	m.close()
	m.a.browse.afterChange(n.ID)
	m.a.say("Note saved.")
}

// confirmDelete asks, then removes the selected note.
func (a *app) confirmDelete(n model.Note) {
	label := notes.Snippet(n.Text)
	if len([]rune(label)) > 50 {
		label = string([]rune(label)[:49]) + "…"
	}
	text := "Delete this note?\n\n" + label
	if a.svc.Cfg.RemoteSyncEnabled && n.RemoteID != "" {
		text += "\n\nIts copy on the server is deleted too."
	}
	modal := tview.NewModal().SetText(text).AddButtons([]string{"Delete", "Cancel"})
	modal.SetBackgroundColor(modalBackground).SetTextColor(tcell.ColorWhite)
	modal.SetButtonStyle(tcell.StyleDefault.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorWhite))
	modal.SetButtonActivatedStyle(tcell.StyleDefault.Background(modalMark).Foreground(tcell.ColorBlack))
	modal.SetBorderColor(modalBorder)
	modal.SetDoneFunc(func(_ int, label string) {
		a.pages.RemovePage(confirmPage)
		a.modal = ""
		a.focusList()
		if label != "Delete" {
			return
		}
		if err := a.svc.Delete(n.ID); err != nil {
			a.say("[" + warn + "]" + tview.Escape(err.Error()))
			return
		}
		a.browse.afterChange(0)
		a.say("Note deleted.")
	})
	a.pages.AddPage(confirmPage, modal, true, true)
	a.modal = confirmPage
	a.tv.SetFocus(modal)
}
