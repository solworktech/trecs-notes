//go:build !linux

package ui

import "errors"

func isUnavailable(err error) bool { return errors.Is(err, ErrNoSettingsWindow) }
