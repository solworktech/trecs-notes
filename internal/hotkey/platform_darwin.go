//go:build darwin

package hotkey

import gohotkey "golang.design/x/hotkey"

func modAlt() gohotkey.Modifier     { return gohotkey.ModOption }
func modSuper() gohotkey.Modifier   { return gohotkey.ModCmd }
func modPrimary() gohotkey.Modifier { return gohotkey.ModCmd } // "CommandOrControl"
