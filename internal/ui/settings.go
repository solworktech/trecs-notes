package ui

// SettingsData is what the Settings window opens with.
type SettingsData struct {
	Hotkey       string
	Width        int
	Height       int
	CopyFallback bool
	Remote       bool
	Server       string // the server to use (the resolved default when none was set)
	Email        string
	Password     string
	LoginStatus  string
	Unsynced     int
	DBPath       string
}

// SettingsResult is what was saved; Saved=false means cancelled.
type SettingsResult struct {
	Hotkey       string
	Width        int
	Height       int
	CopyFallback bool
	Remote       bool
	Server       string
	Email        string
	Password     string
	Saved        bool
}

// Actions the window's buttons trigger.
const (
	ActLogin = iota + 1
	ActLogout
	ActSend
	ActOpenFolder
	ActCheckHotkey
)

// SettingsReply is the handler's answer to an action; the window shows it.
type SettingsReply struct {
	Message     string
	LoginStatus string // "" = leave as is
	Unsynced    int
	OK          bool
}

// SettingsHandler does the work behind the window's buttons (login, send,
// hotkey check...). It runs on the GUI thread, so the window is unresponsive
// while it works; the window shows "Working..." first.
type SettingsHandler func(action int, a1, a2, a3 string) SettingsReply

// IsUnavailable reports whether err means "no settings window here".
func IsUnavailable(err error) bool { return isUnavailable(err) }
