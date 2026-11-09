//go:build linux

package ui

// iconPNG is the app icon, shown in the form's title bar and taskbar entry.
var iconPNG []byte

// SetIcon gives the dialogs the app icon (PNG). Call it before showing one,
// and in the `dialog` child process too.
func SetIcon(png []byte) { iconPNG = png }
