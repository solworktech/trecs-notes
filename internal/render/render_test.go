package render

import (
	"strings"
	"testing"
)

func plainText(runs []Run) string {
	var b strings.Builder
	for _, r := range runs {
		b.WriteString(r.Text)
	}
	return b.String()
}

func find(runs []Run, sub string) *Run {
	for i := range runs {
		if strings.Contains(runs[i].Text, sub) {
			return &runs[i]
		}
	}
	return nil
}

func TestMarkdownInline(t *testing.T) {
	runs := Markdown("Some **bold**, *it*, ~~gone~~ and `code` with [a link](https://x.example/p).")
	for sub, flag := range map[string]int{"bold": Bold, "it": Italic, "gone": Strike, "code": Code, "a link": Link} {
		r := find(runs, sub)
		if r == nil || r.Flags&flag == 0 {
			t.Errorf("%q should have flag %b: %+v", sub, flag, runs)
		}
	}
	if !strings.Contains(plainText(runs), "(https://x.example/p)") {
		t.Errorf("the link target is not shown: %q", plainText(runs))
	}
}

func TestMarkdownBlocks(t *testing.T) {
	src := "# Title\n\nPara one\nsecond line\n\n- a\n- b\n  - nested\n\n1. x\n2. y\n\n> quoted\n\n---\n\n- [x] done\n- [ ] todo\n"
	runs := Markdown(src)
	out := plainText(runs)
	for _, want := range []string{"Title\n", "Para one\nsecond line", "• a\n• b\n", "◦ nested", "1. x\n2. y", "quoted", "────", "☑ done", "☐ todo"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	if r := find(runs, "Title"); r == nil || r.Flags&H1 == 0 {
		t.Error("heading not styled as H1")
	}
	if r := find(runs, "quoted"); r == nil || r.Flags&Quote == 0 {
		t.Error("quote not styled")
	}
	if strings.HasSuffix(out, "\n") {
		t.Error("trailing blank lines were not trimmed")
	}
}

func TestMarkdownCodeBlockIsHighlighted(t *testing.T) {
	runs := Markdown("```go\npackage main\n\n// hi\nfunc main() {}\n```\n")
	var coloured, comment bool
	for _, r := range runs {
		if r.Flags&CodeBlock == 0 {
			t.Errorf("run %q is not part of the code block", r.Text)
		}
		if r.Color != Plain {
			coloured = true
		}
		if r.Color == Comment && strings.Contains(r.Text, "// hi") {
			comment = true
		}
	}
	if !coloured || !comment {
		t.Errorf("the Go block was not highlighted (coloured=%v comment=%v): %+v", coloured, comment, runs)
	}
	if plainText(runs) != "package main\n\n// hi\nfunc main() {}\n" && plainText(runs) != "package main\n\n// hi\nfunc main() {}" {
		t.Errorf("code text changed: %q", plainText(runs))
	}
}

func TestMarkdownTable(t *testing.T) {
	out := plainText(Markdown("| Name | Qty |\n|---|---|\n| apple | 3 |\n| fig | 12 |\n"))
	for _, want := range []string{"Name  │ Qty", "apple │ 3", "fig   │ 12"} {
		if !strings.Contains(out, want) {
			t.Errorf("table row %q missing in\n%s", want, out)
		}
	}
}

func TestHighlightSpans(t *testing.T) {
	src := "x := 1 // note\ny := \"s\"\n/* multi\nline */"
	sp := Highlight(src, "go")
	if len(sp) == 0 {
		t.Fatal("no spans")
	}
	runes := []rune(src)
	got := map[Color]string{}
	for _, s := range sp {
		got[s.Color] += string(runes[s.Start:s.End]) + "|"
	}
	if !strings.Contains(got[Comment], "// note") {
		t.Errorf("line comment not found: %v", got)
	}
	if !strings.Contains(got[Comment], "multi") || !strings.Contains(got[Comment], "line */") {
		t.Errorf("multi-line comment not carried across lines: %v", got)
	}
	if !strings.Contains(got[String], "\"s\"") {
		t.Errorf("string not found: %v", got)
	}
}

func TestDetect(t *testing.T) {
	for _, c := range []struct{ title, text, want string }{
		{"main.go - project - Visual Studio Code", "x", "go"},
		{"~/bin/deploy.sh (~) - VIM", "x", "shell"},
		{"", "#!/usr/bin/env python3\nprint(1)", "python3"},
		{"Inbox", "# Title\n\n- a\n- b", "markdown"},
		{"Inbox", "just some words", ""},
	} {
		got := Detect(c.title, c.text)
		if c.want == "shell" { // the lexer's name for sh/bash varies; accept either
			if got != "shell" && got != "sh" && got != "bash" {
				t.Errorf("Detect(%q) = %q, want a shell", c.title, got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("Detect(%q, %q) = %q, want %q", c.title, c.text, got, c.want)
		}
	}
}

func TestEncode(t *testing.T) {
	got := Encode([]Run{{Text: "a", Flags: Bold}, {Text: "b", Flags: Code, Color: Keyword}})
	want := "1\x1fa\x1e" + "16392\x1fb\x1e" // Code (8) | Keyword (4) << 12
	if got != want {
		t.Errorf("Encode = %q, want %q", got, want)
	}
}

func TestSyntaxesListed(t *testing.T) {
	s := Syntaxes()
	if len(s) < 50 {
		t.Fatalf("only %d syntaxes loaded", len(s))
	}
	seen := map[string]bool{}
	for _, n := range s {
		seen[n] = true
	}
	for _, n := range []string{"go", "markdown", "python3", "yaml", "json"} {
		if !seen[n] {
			t.Errorf("%q missing", n)
		}
	}
}

func TestFenceHintAndResolve(t *testing.T) {
	for text, want := range map[string]string{
		"```go\nfunc main() {}\n```": "go",
		"\n  ```golang\nx\n":         "go",
		"~~~ bash\nls":               "shell",
		"```{.py}\nx=1":              "python",
		"```\nno lang\n```":          "",
		"text first\n```go\n":        "",
		"```nosuchlang\n":            "",
	} {
		if got := FenceHint(text); got != want {
			t.Errorf("FenceHint(%q) = %q, want %q", text, got, want)
		}
	}
	if Resolve("YML") != "yaml" || Resolve("toml") != "toml" {
		t.Errorf("Resolve: %q %q", Resolve("YML"), Resolve("toml"))
	}
}

func TestFenceBody(t *testing.T) {
	body, skip := FenceBody("```go\nx := 1\n```\nafter")
	if body != "x := 1\n" || skip != 6 {
		t.Errorf("got %q, %d", body, skip)
	}
	body, skip = FenceBody("plain\n```\n")
	if body != "plain\n```\n" || skip != 0 {
		t.Errorf("unfenced: %q, %d", body, skip)
	}
}
