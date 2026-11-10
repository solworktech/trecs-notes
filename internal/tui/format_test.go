package tui

import (
	"strings"
	"testing"

	"github.com/solworktech/trecs-notes/internal/render"
)

func TestRawTextKeepsEveryRune(t *testing.T) {
	text := "x := \"héllo [red]\" // ok\n"
	_, spans := render.Auto("main.go - editor", text)
	got := rawText(text, spans)
	if len(spans) == 0 || !strings.Contains(got, "[#") {
		t.Fatalf("expected colours, got %q", got)
	}
	plain := tviewStrip(got)
	if plain != text {
		t.Errorf("text changed: %q", plain)
	}
}

func TestMarkdownText(t *testing.T) {
	got := markdownText(render.Markdown("# Title\n\nSome **bold** and `code` [x](http://y)\n"))
	for _, want := range []string{"Title", "bold", "code", "x"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if !strings.Contains(got, ":b]") {
		t.Errorf("no bold in %q", got)
	}
}
