//go:build windows

package hotkey

import gohotkey "golang.design/x/hotkey"

func modAlt() gohotkey.Modifier   { return gohotkey.ModAlt }
func modSuper() gohotkey.Modifier { return gohotkey.ModWin }
