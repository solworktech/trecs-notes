//go:build !linux

package gui

// Available says whether there is a graphical session. On macOS and Windows
// a person running the program has one.
func Available() error { return nil }
