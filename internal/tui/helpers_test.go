package tui

import (
	"regexp"
	"strings"
)

var styleTag = regexp.MustCompile(`\[(?:-|#[0-9a-fA-F]{6})?:-:[a-z-]+\]`)

// tviewStrip removes the colour tags and the escapes of tview text, leaving
// what a person would read.
func tviewStrip(s string) string {
	s = styleTag.ReplaceAllString(s, "")
	return strings.ReplaceAll(s, "[red[]", "[red]")
}
