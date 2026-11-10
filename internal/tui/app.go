// Package tui is the terminal frontend, for machines with no graphical
// session (or when asked for with -tui). Like the graphical one it holds no
// logic of its own about notes or settings - that is internal/notes - only
// what is about the screen and the keys. Its look, and the way a page has a
// legend of keys and opens modals over itself, follow trecs-edit.
package tui

import (
	"context"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/solworktech/trecs-notes/internal/notes"
)

const (
	mainPage     = "main"
	settingsPage = "settings"
	helpPage     = "help"
)

type app struct {
	svc    *notes.Service
	tv     *tview.Application
	pages  *tview.Pages
	status *tview.TextView
	browse *browser

	settings *settingsModal
	edit     *editModal
	modal    string // the modal that is open: settingsPage, helpPage, editPage, confirmPage, or ""
	helpBack string // the page the help was opened from
	msg      string // what the status line says, until the person does something else
}

// Run shows the terminal interface until the person quits.
func Run(s *notes.Service) error {
	a := &app{svc: s, tv: tview.NewApplication()}

	legend := newLegendBar("F1",
		legendItem{"^E", "Edit", 1},
		legendItem{"Del", "Delete", 2},
		legendItem{"F3", "Raw/Markdown", 4},
		legendItem{"F2", "Settings", 3},
		legendItem{"F5", "Sync", 5},
		legendItem{"Q", "Quit", 2},
	)
	legend.find = true
	a.status = tview.NewTextView().SetDynamicColors(true)
	a.browse = newBrowser(a)

	main := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(legend, 1, 0, false).
		AddItem(a.browse.primitive(), 0, 1, true).
		AddItem(a.status, 1, 0, false)

	a.pages = tview.NewPages().AddPage(mainPage, main, true, true)
	a.settings = newSettingsModal(a)
	a.edit = newEditModal(a)

	a.tv.SetInputCapture(a.handleInput)
	a.tv.SetFocus(a.browse.list)
	a.browse.focusBorders()
	a.tv.SetBeforeDrawFunc(func(tcell.Screen) bool { a.browse.focusBorders(); return false })
	if v, err := s.DB.DataVersion(context.Background()); err == nil {
		a.browse.version = v
	}
	a.browse.searchNow()

	stop := make(chan struct{})
	defer close(stop)
	go a.browse.watch(stop)

	return a.tv.SetRoot(a.pages, true).EnableMouse(true).Run()
}

// refreshStatus writes the line at the bottom: what the person was last
// told, or else how many notes there are and where syncing stands.
func (a *app) refreshStatus() {
	if a.msg != "" {
		a.status.SetText(a.msg)
		return
	}
	line, attention := a.svc.StatusLine()
	colour := dim
	if attention {
		colour = warn
		line += " (F5 sends them)"
	}
	text := "[" + colour + "]" + tview.Escape(line)
	a.status.SetText(text)
}

// say shows a line at the bottom of the main page.
func (a *app) say(msg string) {
	a.msg = msg
	a.refreshStatus()
}

func (a *app) clearMessage() {
	if a.msg != "" {
		a.msg = ""
		a.refreshStatus()
	}
}

func (a *app) focusList() {
	a.tv.SetFocus(a.browse.list)
	a.browse.focusBorders()
}

// refreshBody is what the settings modal calls when it has changed something
// that the main page shows.
func (a *app) refreshBody() {
	a.browse.afterRefresh()
}

// syncNow sends what the server has not seen, away from the screen's goroutine.
func (a *app) syncNow() {
	if !a.svc.Cfg.RemoteSyncEnabled {
		a.say("Syncing is off: turn it on in Settings (F2).")
		return
	}
	a.say("[" + dim + "]Sending...")
	go func() {
		r := notes.SendUnsynced(a.svc.DB)
		a.tv.QueueUpdateDraw(func() {
			colour := "white"
			if !r.OK {
				colour = warn
			}
			a.say("[" + colour + "]" + tview.Escape(r.Message))
			a.browse.afterRefresh()
		})
	}()
}

func (a *app) handleInput(ev *tcell.EventKey) *tcell.EventKey {
	switch a.modal {
	case helpPage:
		switch ev.Key() {
		case tcell.KeyEscape, tcell.KeyEnter, tcell.KeyF1:
			a.closeHelp()
			return nil
		case tcell.KeyRune:
			switch ev.Rune() {
			case '?', 'q', 'Q':
				a.closeHelp()
				return nil
			}
		case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyHome, tcell.KeyEnd:
			return ev
		}
		return nil
	case settingsPage, editPage:
		if ev.Key() == tcell.KeyF1 {
			a.openHelp(a.modal)
			return nil
		}
		return ev // the modal handles the rest
	case confirmPage:
		if ev.Key() == tcell.KeyEscape {
			a.pages.RemovePage(confirmPage)
			a.modal = ""
			a.focusList()
			return nil
		}
		return ev
	}

	b := a.browse
	typing := b.typing()
	switch ev.Key() {
	case tcell.KeyF1:
		a.openHelp(mainPage)
		return nil
	case tcell.KeyF2:
		a.settings.open()
		return nil
	case tcell.KeyF3:
		b.toggleMarkdown()
		return nil
	case tcell.KeyF5:
		a.syncNow()
		return nil
	case tcell.KeyCtrlF:
		a.tv.SetFocus(b.find)
		return nil
	case tcell.KeyCtrlE:
		if n, ok := b.current(); ok {
			a.edit.open(n)
		}
		return nil
	case tcell.KeyDelete:
		if !typing {
			if n, ok := b.current(); ok {
				a.confirmDelete(n)
			}
			return nil
		}
	case tcell.KeyTab:
		b.focusNext(1)
		return nil
	case tcell.KeyBacktab:
		b.focusNext(-1)
		return nil
	case tcell.KeyEscape:
		switch {
		case b.find.HasFocus() && b.find.GetText() != "":
			b.find.SetText("")
		default:
			a.focusList()
		}
		return nil
	case tcell.KeyEnter:
		if b.find.HasFocus() {
			a.focusList()
			return nil
		}
	case tcell.KeyDown:
		if b.find.HasFocus() {
			a.focusList()
			return nil
		}
	case tcell.KeyRune:
		if ev.Modifiers()&tcell.ModAlt != 0 {
			switch ev.Rune() {
			case 'i', 'I':
				b.toggleOption(b.matchCase)
				return nil
			case 'r', 'R':
				b.toggleOption(b.regex)
				return nil
			}
		}
		if typing {
			return ev
		}
		switch ev.Rune() {
		case '/':
			a.tv.SetFocus(b.find)
			return nil
		case '?':
			a.openHelp(mainPage)
			return nil
		case 'q', 'Q':
			a.tv.Stop()
			return nil
		}
	}
	return ev
}

func helpRows(page string) (title string, rows []helpRow) {
	if page == settingsPage {
		return "Settings", []helpRow{
			{"Tab / Shift+Tab", "Move between the fields and the buttons"},
			{"Space", "Switch a checkbox"},
			{"Hotkeys", "For the tray app, which restarts itself when they change"},
			{"Copy fallback", "Try the copy shortcut when an application reports no selection"},
			{"Sync to server", "Also send each new note to the trecs server"},
			{"Sync notes to server", "Send the notes the server does not have yet"},
			{"^S", "Save the settings"},
			{"Esc", "Close without saving"},
			{"F1", "This help"},
			{"^C", "Quit"},
		}
	}
	if page == editPage {
		return "Edit note", []helpRow{
			{"Tab / Shift+Tab", "Move between the fields and the buttons"},
			{"Tags", "Separated by commas or spaces"},
			{"^E", "In the Text box: edit the text in $VISUAL or $EDITOR"},
			{"^S", "Save the note"},
			{"Esc", "Close without saving"},
			{"F1", "This help"},
			{"^C", "Quit"},
		}
	}
	return "Trecs Notes", []helpRow{
		{"/ or ^F", "Find: type to search as you go"},
		{"words", "Whole words, all of which must appear; the last may be a beginning"},
		{"\"a phrase\"", "Words together, in that order"},
		{"tag:k8s", "Notes with that tag (repeat for more)"},
		{"app:firefox", "Notes from an app whose name contains this"},
		{"since:2026-01-31", "Saved on or after this day (until: for the other end)"},
		{"Alt+R", "Regex: what is left of the text is a regular expression"},
		{"Alt+I", "Match case (off: case counts only if the text has a capital)"},
		{"Tab / Shift+Tab", "Move between the find box, the list and the note"},
		{"Up, Down, PgUp, PgDn", "Move in the list, or scroll the note when it has the keys"},
		{"Enter", "From the list, go to the note, to scroll it; from Find, to the list"},
		{"^E", "Edit the selected note"},
		{"Del", "Delete the selected note (it asks first)"},
		{"F3", "Show the note as Raw text or as Markdown"},
		{"F5", "Send changes the server has not seen (when syncing is on)"},
		{"F2", "Settings"},
		{"Esc", "Clear Find, or go back to the list"},
		{"? / F1", "This help"},
		{"Q / ^C", "Quit"},
	}
}

func (a *app) openHelp(page string) {
	title, rows := helpRows(page)
	frame, text := newHelp(title, rows)
	a.helpBack = page
	if a.pages.HasPage(helpPage) {
		a.pages.RemovePage(helpPage)
	}
	a.pages.AddPage(helpPage, frame, true, true)
	a.modal = helpPage
	a.tv.SetFocus(text)
}

func (a *app) closeHelp() {
	a.pages.RemovePage(helpPage)
	switch a.helpBack {
	case settingsPage:
		a.modal = settingsPage
		a.settings.focus()
	case editPage:
		a.modal = editPage
		a.edit.focus()
	default:
		a.modal = ""
		a.focusList()
	}
}
