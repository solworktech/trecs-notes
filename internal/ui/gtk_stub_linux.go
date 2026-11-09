//go:build linux && nogtk

package ui

// Built with -tags nogtk (no libgtk-3-dev on the build machine): the form
// falls back to yad, then zenity.
func runGTK(in Input) (Result, error) { return Result{}, errGTKUnavailable }

func ShowSettings(d SettingsData, h SettingsHandler) (SettingsResult, error) {
	return SettingsResult{}, errGTKUnavailable
}
