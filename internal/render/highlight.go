package render

import (
	"embed"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	highlight "github.com/jessp01/gohighlight"
)

//go:embed syntax_files/*.yaml
var syntaxFS embed.FS

var (
	loadOnce sync.Once
	defs     []*highlight.Def
	colorOf  map[highlight.Group]Color
)

// load parses the embedded syntax definitions, once. It takes a moment, so
// nothing calls it until highlighting is first asked for.
func load() {
	loadOnce.Do(func() {
		entries, _ := syntaxFS.ReadDir("syntax_files")
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			data, err := syntaxFS.ReadFile("syntax_files/" + e.Name())
			if err != nil || len(data) == 0 {
				continue
			}
			d, err := highlight.ParseDef(data)
			if err == nil && d != nil && d.FileType == "" {
				// python3.yaml upstream says "filename:" where it means
				// "filetype:"; the file's own name is the fallback
				d.FileType = strings.TrimSuffix(e.Name(), ".yaml")
			}
			if err != nil || d == nil || d.FileType == "" || !detectable(d) {
				continue
			}
			defs = append(defs, d)
		}
		highlight.ResolveIncludes(defs)

		colorOf = map[highlight.Group]Color{}
		for name, g := range highlight.Groups {
			colorOf[g] = colorFor(name)
		}
	})
}

// detectable guards against a definition that declares no way of being
// detected: DetectFiletype indexes into that list unconditionally.
func detectable(d *highlight.Def) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	highlight.DetectFiletype([]*highlight.Def{d}, "x", nil)
	return true
}

func colorFor(group string) Color {
	g := strings.ToLower(group)
	switch {
	case strings.HasPrefix(g, "comment"):
		return Comment
	case g == "constant.string" || strings.HasPrefix(g, "constant.string"):
		return String
	case g == "constant.number" || g == "constant.bool" || strings.HasPrefix(g, "constant"):
		return Number
	case strings.HasPrefix(g, "statement") || g == "keyword":
		return Keyword
	case strings.HasPrefix(g, "type"):
		return Type
	case strings.HasPrefix(g, "identifier"):
		return Ident
	case strings.HasPrefix(g, "preproc"):
		return Preproc
	case strings.HasPrefix(g, "special"):
		return Special
	case strings.HasPrefix(g, "symbol"):
		return Symbol
	}
	return Plain
}

// Syntaxes lists the names of the languages that can be highlighted.
func Syntaxes() []string {
	load()
	seen := map[string]bool{}
	var out []string
	for _, d := range defs {
		if !seen[d.FileType] {
			seen[d.FileType] = true
			out = append(out, d.FileType)
		}
	}
	sort.Strings(out)
	return out
}

func defByName(name string) *highlight.Def {
	load()
	for _, d := range defs {
		if strings.EqualFold(d.FileType, name) {
			return d
		}
	}
	return nil
}

// aliases maps the names people write after a code fence to lexer names.
var aliases = map[string]string{
	"golang": "go", "sh": "shell", "bash": "shell", "ksh": "shell",
	"console": "shell", "js": "javascript", "jsx": "javascript",
	"node": "javascript", "ts": "typescript", "tsx": "typescript",
	"py": "python", "python3": "python", "rb": "ruby", "yml": "yaml",
	"cs": "csharp", "c#": "csharp", "cpp": "c++", "cxx": "c++",
	"cc": "c++", "objc": "objective-c", "rs": "rust", "md": "markdown",
	"docker": "dockerfile", "make": "makefile", "htm": "html",
	"pl": "perl", "ps1": "powershell", "tex": "tex", "latex": "tex",
}

// Resolve turns a language name as written in a fence ("go", "golang",
// "py", "sh") into the name of a lexer, or "" if there is none.
func Resolve(name string) string {
	load()
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	if a, ok := aliases[name]; ok {
		name = a
	}
	if d := defByName(name); d != nil {
		return d.FileType
	}
	// as a file extension: "toml", "conf", "proto"...
	if d := highlight.DetectFiletype(defs, "x."+name, nil); d != nil && d.FileType != "Unknown" {
		return d.FileType
	}
	return ""
}

var fenceOpen = regexp.MustCompile("^\\s*(?:```+|~~~+)\\s*\\{?\\.?([A-Za-z0-9_+#.-]+)")

// FenceBody splits a note that is wrapped in a code fence ("```go" ... "```")
// into the code between the fences. skip is how many runes precede it, so
// that spans found in body can be moved back into the whole text. A note
// without a fence on its first non-blank line is returned whole.
func FenceBody(text string) (body string, skip int) {
	lines := strings.SplitAfter(text, "\n")
	first := -1
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			first = i
			break
		}
	}
	if first < 0 || !fenceOpen.MatchString(lines[first]) {
		return text, 0
	}
	n := 0
	for _, l := range lines[:first+1] {
		n += utf8.RuneCountInString(l)
	}
	rest := lines[first+1:]
	end := len(rest)
	for i, l := range rest {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			end = i
			break
		}
	}
	return strings.Join(rest[:end], ""), n
}

// FenceHint returns the lexer named by a code fence on the first non-blank
// line ("```go"), or "". Such a note says outright what it contains.
func FenceHint(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if m := fenceOpen.FindStringSubmatch(line); m != nil {
			return Resolve(m[1])
		}
		return ""
	}
	return ""
}

// Span colours the runes [Start, End) of the text.
type Span struct {
	Start, End int
	Color      Color
}

// Highlight returns the coloured spans of text in the named language (empty
// or unknown: none). Offsets count runes, as GTK's text iterators do.
func Highlight(text, syntax string) []Span {
	d := defByName(syntax)
	if d == nil || text == "" {
		return nil
	}
	return spans(text, d)
}

// Colour is Highlight for a whole note: one wrapped in a code fence is
// coloured inside it, so that the fence itself does not open a string in the
// language. Both frontends call this.
func Colour(text, syntax string) []Span {
	body, skip := FenceBody(text)
	sp := Highlight(body, syntax)
	for i := range sp {
		sp[i].Start += skip
		sp[i].End += skip
	}
	return sp
}

// Auto picks the language of a note - what its code fence names, else what
// Detect finds from the window title and the text - and colours it.
func Auto(title, text string) (syntax string, spans []Span) {
	syntax = FenceHint(text)
	if syntax == "" {
		syntax = Detect(title, text)
	}
	if syntax == "" {
		return "", nil
	}
	return syntax, Colour(text, syntax)
}

func spans(text string, d *highlight.Def) []Span {
	matches := highlight.NewHighlighter(d).HighlightString(text)
	var out []Span
	offset := 0
	for n, line := range strings.Split(text, "\n") {
		lineLen := len([]rune(line))
		if n < len(matches) && len(matches[n]) > 0 {
			cols := make([]int, 0, len(matches[n]))
			for c := range matches[n] {
				cols = append(cols, c)
			}
			sort.Ints(cols)
			for i, c := range cols {
				end := lineLen
				if i+1 < len(cols) {
					end = cols[i+1]
				}
				if c >= lineLen || end <= c {
					continue
				}
				col := colorOf[matches[n][c]]
				if col == Plain {
					continue
				}
				// the lexer reports a change at every rule boundary (a
				// comment's marker and its text, say): join touching runs
				if k := len(out) - 1; k >= 0 && out[k].Color == col && out[k].End == offset+c {
					out[k].End = offset + end
				} else {
					out = append(out, Span{offset + c, offset + end, col})
				}
			}
		}
		offset += lineLen + 1
	}
	return out
}

// EncodeSpans is the wire form for the GUI: "start:end:color" joined by ";".
func EncodeSpans(sp []Span) string {
	var b strings.Builder
	for i, s := range sp {
		if i > 0 {
			b.WriteByte(';')
		}
		b.Write(appendInt(nil, s.Start))
		b.WriteByte(':')
		b.Write(appendInt(nil, s.End))
		b.WriteByte(':')
		b.Write(appendInt(nil, int(s.Color)))
	}
	return b.String()
}

var filenameLike = regexp.MustCompile(`[A-Za-z0-9_@+\-][A-Za-z0-9_@+.\-]*\.[A-Za-z0-9]{1,12}\b`)

// Detect guesses the language of a snippet: from a file name in the window
// title ("main.go - Visual Studio Code"), from a first line such as a
// #! line, and finally from whether the text looks like Markdown. It returns
// "" when it has no idea.
func Detect(title, text string) string {
	load()
	firstLine, _, _ := strings.Cut(text, "\n")
	for _, name := range filenameLike.FindAllString(title, -1) {
		if d := highlight.DetectFiletype(defs, name, nil); d != nil && d.FileType != "Unknown" {
			return d.FileType
		}
	}
	if d := highlight.DetectFiletype(defs, "", []byte(firstLine)); d != nil && d.FileType != "Unknown" {
		return d.FileType
	}
	if LooksLikeMarkdown(text) {
		return "markdown"
	}
	return ""
}

var mdSignals = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^#{1,6} \S`),
	regexp.MustCompile("(?m)^```"),
	regexp.MustCompile(`\[[^\]\n]+\]\([^)\n]+\)`),
	regexp.MustCompile(`(?m)^\s*([-*+]|\d+\.) \S`),
	regexp.MustCompile(`\*\*[^*\n]+\*\*`),
	regexp.MustCompile("`[^`\n]+`"),
	regexp.MustCompile(`(?m)^> \S`),
}

// LooksLikeMarkdown is a heuristic: two different kinds of Markdown marking
// (a heading and a list, a link and emphasis...) make it Markdown.
func LooksLikeMarkdown(text string) bool {
	n := 0
	for _, re := range mdSignals {
		if re.MatchString(text) {
			n++
		}
	}
	return n >= 2
}
