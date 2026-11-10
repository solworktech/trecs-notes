package tui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/solworktech/trecs-notes/internal/config"
	"github.com/solworktech/trecs-notes/internal/notes"
)

const (
	lblCopy = "Copy fallback"
	lblSync = "Sync to server"
)

// settingsModal edits the settings, which are the same file the graphical
// Settings window writes (config.json), through the same internal/notes code.
type settingsModal struct {
	a       *app
	frame   tview.Primitive
	form    *tview.Form
	paths   *tview.TextView // where the notes and the config file are
	login   *tview.TextView // who the notes are sent as
	message *tview.TextView
	base    notes.Form // what was loaded: the settings this screen does not show are kept
}

func newSettingsModal(a *app) *settingsModal {
	m := &settingsModal{a: a}

	m.form = tview.NewForm()
	m.form.SetBackgroundColor(modalBackground).SetBorderPadding(0, 0, 0, 0)
	m.form.SetFieldBackgroundColor(tcell.ColorDarkSlateGray).SetFieldTextColor(tcell.ColorWhite)
	m.form.SetLabelColor(tcell.ColorWhite)
	m.form.SetButtonStyle(tcell.StyleDefault.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorWhite))
	m.form.SetButtonActivatedStyle(tcell.StyleDefault.Background(modalMark).Foreground(tcell.ColorBlack))
	m.form.SetButtonsAlign(tview.AlignLeft).SetItemPadding(1)

	hint := tview.NewTextView().SetDynamicColors(true).SetWrap(true).
		SetText("[" + dim + "]The tray restarts itself when hotkeys change. Empty Quick save = off.")
	hint.SetBackgroundColor(modalBackground)

	m.paths = tview.NewTextView().SetDynamicColors(true)
	m.paths.SetBackgroundColor(modalBackground)
	m.login = tview.NewTextView().SetDynamicColors(true)
	m.login.SetBackgroundColor(modalBackground)
	m.message = tview.NewTextView().SetDynamicColors(true)
	m.message.SetBackgroundColor(modalBackground)

	box := opaque(tview.NewFlex(), true).SetDirection(tview.FlexRow).
		AddItem(hint, 1, 0, false).
		AddItem(gap(), 1, 0, false).
		AddItem(m.form, 0, 1, true).
		AddItem(gap(), 1, 0, false).
		AddItem(m.paths, 2, 0, false).
		AddItem(m.login, 1, 0, false).
		AddItem(m.message, 1, 0, false)
	box.SetBorder(true).SetBorderColor(modalBorder).SetTitle(" Settings ").SetBorderPadding(0, 0, 1, 1)
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
		}
		return ev
	})
	return m
}

// load fills the form from the config file, as it is now.
func (m *settingsModal) load() error {
	f, err := notes.LoadForm()
	if err != nil {
		return err
	}
	m.base = f
	m.form.Clear(true)
	m.form.AddInputField("Hotkey", f.Hotkey, 30, nil, nil)
	m.form.AddInputField("Quick save", f.QuickHotkey, 30, nil, nil)
	m.form.AddCheckbox(lblCopy, f.CopyFallback, nil)
	m.form.AddCheckbox(lblSync, f.RemoteSyncEnabled, nil)
	m.form.AddInputField("Server", f.Server, 34, nil, nil)
	m.form.AddInputField("Email", f.Email, 34, nil, nil)
	m.form.AddPasswordField("Password", f.Password, 34, '*', nil)
	m.form.AddButton("Save ^S", m.save)
	m.form.AddButton("Log in", m.doLogin)
	m.form.AddButton("Log out", m.doLogout)
	m.form.AddButton("Sync notes to server", m.doSend)
	m.form.AddButton("Cancel Esc", m.close)
	m.form.SetFocus(0)
	dbPath, _ := config.DBPath()
	cfgPath, _ := config.Path()
	m.paths.SetText("[" + dim + "]Database     [white]" + tview.Escape(dbPath) + "\n[" + dim + "]Config file  [white]" + tview.Escape(cfgPath))
	m.login.SetText(notes.LoginStatus())
	m.message.SetText("")
	return nil
}

func (m *settingsModal) open() {
	if err := m.load(); err != nil {
		m.a.say("[" + warn + "]Settings: " + tview.Escape(err.Error()))
		return
	}
	m.a.pages.AddPage(settingsPage, m.frame, true, true)
	m.a.modal = settingsPage
	m.focus()
}

func (m *settingsModal) focus() { m.a.tv.SetFocus(m.form) }

func (m *settingsModal) close() {
	m.a.pages.RemovePage(settingsPage)
	m.a.modal = ""
}

func (m *settingsModal) text(label string) string {
	return m.form.GetFormItemByLabel(label).(*tview.InputField).GetText()
}

func (m *settingsModal) checked(label string) bool {
	return m.form.GetFormItemByLabel(label).(*tview.Checkbox).IsChecked()
}

// current is the form as it is on the screen. What it does not show (the size
// of the graphical form) is as it was loaded.
func (m *settingsModal) current() notes.Form {
	f := m.base
	f.Hotkey = strings.TrimSpace(m.text("Hotkey"))
	f.QuickHotkey = strings.TrimSpace(m.text("Quick save"))
	f.CopyFallback = m.checked(lblCopy)
	f.RemoteSyncEnabled = m.checked(lblSync)
	f.Server = m.text("Server")
	f.Email = m.text("Email")
	f.Password = m.form.GetFormItemByLabel("Password").(*tview.InputField).GetText()
	return f
}

func (m *settingsModal) fail(err error) {
	m.message.SetText("[" + warn + "]" + tview.Escape(err.Error()))
}

func (m *settingsModal) save() {
	f := m.current()
	if err := notes.SaveForm(f); err != nil {
		m.fail(err)
		return
	}
	m.a.svc.Reload()
	msg := "Settings saved."
	if notes.HotkeysChanged(m.base.Config, f.Config) {
		msg += " Restart the tray app to use the new hotkeys."
	}
	m.close()
	m.a.refreshBody()
	m.a.say(msg)
}

// run does a slow action (a network call) away from the screen's goroutine.
func (m *settingsModal) run(work func() notes.Reply) {
	m.message.SetText("[" + dim + "]Working...")
	go func() {
		r := work()
		m.a.tv.QueueUpdateDraw(func() {
			colour := "white"
			if !r.OK {
				colour = warn
			}
			m.message.SetText("[" + colour + "]" + tview.Escape(r.Message))
			if r.LoginStatus != "" {
				m.login.SetText(r.LoginStatus)
			}
			m.a.refreshBody()
		})
	}()
}

func (m *settingsModal) doLogin() {
	server, email := m.text("Server"), m.text("Email")
	password := m.form.GetFormItemByLabel("Password").(*tview.InputField).GetText()
	m.run(func() notes.Reply { return notes.Login(server, email, password) })
}

func (m *settingsModal) doLogout() { m.run(notes.Logout) }

func (m *settingsModal) doSend() {
	m.run(func() notes.Reply { return notes.SendUnsynced(m.a.svc.DB) })
}

// gap is an empty row.
func gap() *tview.Box { return tview.NewBox().SetBackgroundColor(modalBackground) }
