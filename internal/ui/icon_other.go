//go:build !linux

package ui

// SetIcon: the native dialogs on this platform take their icon from the
// application itself, so there is nothing to keep.
func SetIcon(_ []byte) {}
