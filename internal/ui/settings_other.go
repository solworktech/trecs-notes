//go:build !linux

package ui

import "errors"

// ErrNoSettingsWindow: no native settings window on this platform yet; the
// caller falls back to opening the config file.
var ErrNoSettingsWindow = errors.New("no settings window on this platform")

func ShowSettings(d SettingsData, h SettingsHandler) (SettingsResult, error) {
	return SettingsResult{}, ErrNoSettingsWindow
}
