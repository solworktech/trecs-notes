//go:build linux

package hotkey

import gohotkey "golang.design/x/hotkey"

// On X11, golang.design/x/hotkey exposes the raw modifier masks: Mod1 is
// Alt and Mod4 is Super on a standard keymap.
func modAlt() gohotkey.Modifier     { return gohotkey.Mod1 }
func modSuper() gohotkey.Modifier   { return gohotkey.Mod4 }
func modPrimary() gohotkey.Modifier { return gohotkey.ModCtrl } // "CommandOrControl"
