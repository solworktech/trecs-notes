package render

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

var md = goldmark.New(goldmark.WithExtensions(extension.GFM))

// Markdown renders Markdown source to styled runs: headings, emphasis, lists,
// quotes, tables, links and code, with fenced code highlighted in its own
// language. Line breaks in a paragraph are kept, as on GitHub, since notes
// are rarely written with hard-wrapped paragraphs in mind.
func Markdown(source string) []Run {
	src := []byte(source)
	doc := md.Parser().Parse(text.NewReader(src))
	r := &renderer{src: src}
	r.blocks(doc, 0, 0)
	return r.trimmed()
}

type renderer struct {
	src  []byte
	runs []Run
}

func (r *renderer) emit(s string, flags int) {
	if s == "" {
		return
	}
	if n := len(r.runs); n > 0 && r.runs[n-1].Flags == flags && r.runs[n-1].Color == Plain {
		r.runs[n-1].Text += s
		return
	}
	r.runs = append(r.runs, Run{Text: s, Flags: flags})
}

func (r *renderer) emitColor(s string, flags int, c Color) {
	if s != "" {
		r.runs = append(r.runs, Run{Text: s, Flags: flags, Color: c})
	}
}

// trimmed drops the blank lines left at the end.
func (r *renderer) trimmed() []Run {
	for len(r.runs) > 0 {
		last := &r.runs[len(r.runs)-1]
		last.Text = strings.TrimRight(last.Text, "\n")
		if last.Text != "" {
			break
		}
		r.runs = r.runs[:len(r.runs)-1]
	}
	return r.runs
}

// blocks renders the children of a container; flags carry the quote style
// down, depth the list nesting.
func (r *renderer) blocks(parent ast.Node, flags, depth int) {
	first := true
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		if !first && !tight(parent, n) {
			r.emit("\n", flags)
		}
		first = false
		r.block(n, flags, depth)
	}
}

// tight is true between the paragraphs of a tight list item, which are not
// separated by a blank line.
func tight(parent, n ast.Node) bool {
	if _, ok := parent.(*ast.ListItem); !ok {
		return false
	}
	if l, ok := parent.Parent().(*ast.List); ok && l.IsTight {
		return true
	}
	return false
}

func (r *renderer) block(n ast.Node, flags, depth int) {
	switch v := n.(type) {
	case *ast.Heading:
		h := H3
		switch v.Level {
		case 1:
			h = H1
		case 2:
			h = H2
		}
		r.inlines(v, flags|h)
		r.emit("\n", flags)
	case *ast.Paragraph, *ast.TextBlock:
		r.inlines(v, flags)
		r.emit("\n", flags)
	case *ast.List:
		r.list(v, flags, depth)
	case *ast.FencedCodeBlock:
		r.codeBlock(v, string(v.Language(r.src)), flags)
	case *ast.CodeBlock:
		r.codeBlock(v, "", flags)
	case *ast.Blockquote:
		r.blocks(v, flags|Quote, depth)
	case *ast.ThematicBreak:
		r.emit(strings.Repeat("─", 32)+"\n", flags)
	case *ast.HTMLBlock:
		var b strings.Builder
		for i := 0; i < v.Lines().Len(); i++ {
			s := v.Lines().At(i)
			b.Write(s.Value(r.src))
		}
		r.emit(strings.TrimRight(b.String(), "\n")+"\n", flags)
	case *east.Table:
		r.table(v, flags)
	default:
		r.blocks(n, flags, depth)
	}
}

func (r *renderer) list(l *ast.List, flags, depth int) {
	number := l.Start
	if number == 0 {
		number = 1
	}
	for item := l.FirstChild(); item != nil; item = item.NextSibling() {
		marker := "• "
		if depth%2 == 1 {
			marker = "◦ "
		}
		if l.IsOrdered() {
			marker = fmt.Sprintf("%d. ", number)
			number++
		}
		r.emit(strings.Repeat("    ", depth)+marker, flags)
		r.blocks(item, flags, depth+1)
	}
}

func (r *renderer) codeBlock(n ast.Node, lang string, flags int) {
	var b strings.Builder
	for i := 0; i < n.Lines().Len(); i++ {
		s := n.Lines().At(i)
		b.Write(s.Value(r.src))
	}
	code := strings.TrimRight(b.String(), "\n")
	f := flags | Code | CodeBlock
	if d := defByName(Resolve(lang)); d != nil && code != "" {
		r.highlighted(code, f, spans(code, d))
	} else {
		r.emit(code, f)
	}
	r.emit("\n", flags|Code|CodeBlock)
}

// highlighted emits code with the given colour spans applied.
func (r *renderer) highlighted(code string, flags int, sp []Span) {
	runes := []rune(code)
	pos := 0
	for _, s := range sp {
		if s.Start > pos {
			r.emit(string(runes[pos:s.Start]), flags)
		}
		if s.Start < pos { // overlapping spans: the earlier one wins
			s.Start = pos
		}
		if s.End > s.Start {
			r.emitColor(string(runes[s.Start:s.End]), flags, s.Color)
			pos = s.End
		}
	}
	if pos < len(runes) {
		r.emit(string(runes[pos:]), flags)
	}
}

func (r *renderer) table(t *east.Table, flags int) {
	var rows [][]string
	var header []bool
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		var cells []string
		for c := row.FirstChild(); c != nil; c = c.NextSibling() {
			cells = append(cells, strings.TrimSpace(r.plain(c)))
		}
		rows = append(rows, cells)
		_, isHeader := row.(*east.TableHeader)
		header = append(header, isHeader)
	}
	var width []int
	for _, cells := range rows {
		for i, c := range cells {
			if i >= len(width) {
				width = append(width, 0)
			}
			if w := utf8.RuneCountInString(c); w > width[i] {
				width[i] = w
			}
		}
	}
	line := func(cells []string, f int) {
		var b strings.Builder
		for i, c := range cells {
			if i > 0 {
				b.WriteString(" │ ")
			}
			b.WriteString(c)
			b.WriteString(strings.Repeat(" ", width[i]-utf8.RuneCountInString(c)))
		}
		r.emit(strings.TrimRight(b.String(), " ")+"\n", f)
	}
	for i, cells := range rows {
		if header[i] {
			line(cells, flags|Code|Bold)
			var rule []string
			for _, w := range width {
				rule = append(rule, strings.Repeat("─", w))
			}
			r.emit(strings.Join(rule, "─┼─")+"\n", flags|Code)
		} else {
			line(cells, flags|Code)
		}
	}
}

// plain is the text of a node without any styling.
func (r *renderer) plain(n ast.Node) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := c.(type) {
		case *ast.Text:
			b.Write(v.Segment.Value(r.src))
		case *ast.String:
			b.Write(v.Value)
		case *ast.AutoLink:
			b.Write(v.URL(r.src))
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

func (r *renderer) inlines(parent ast.Node, flags int) {
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		r.inline(n, flags)
	}
}

func (r *renderer) inline(n ast.Node, flags int) {
	switch v := n.(type) {
	case *ast.Text:
		r.emit(string(v.Segment.Value(r.src)), flags)
		if v.HardLineBreak() || v.SoftLineBreak() {
			r.emit("\n", flags)
		}
	case *ast.String:
		r.emit(string(v.Value), flags)
	case *ast.CodeSpan:
		r.emit(strings.ReplaceAll(r.plain(v), "\n", " "), flags|Code)
	case *ast.Emphasis:
		if v.Level >= 2 {
			r.inlines(v, flags|Bold)
		} else {
			r.inlines(v, flags|Italic)
		}
	case *east.Strikethrough:
		r.inlines(v, flags|Strike)
	case *ast.Link:
		label := r.plain(v)
		r.inlines(v, flags|Link)
		if u := string(v.Destination); u != "" && u != label {
			r.emit(" ("+u+")", flags)
		}
	case *ast.AutoLink:
		r.emit(string(v.URL(r.src)), flags|Link)
	case *ast.Image:
		r.emit("[image: "+r.plain(v)+"]", flags|Italic)
	case *east.TaskCheckBox:
		if v.IsChecked {
			r.emit("☑ ", flags)
		} else {
			r.emit("☐ ", flags)
		}
	case *ast.RawHTML:
		for i := 0; i < v.Segments.Len(); i++ {
			s := v.Segments.At(i)
			r.emit(string(s.Value(r.src)), flags)
		}
	default:
		r.inlines(n, flags)
	}
}
