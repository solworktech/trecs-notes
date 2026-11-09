// Package render turns note text into styled runs for display: syntax
// highlighting for the raw text (jessp01/gohighlight) and a Markdown preview
// (goldmark). It knows nothing about GTK; the capture form asks for the runs
// and applies them as text tags.
package render

// Color is a syntax colour class; the GUI maps each to a concrete colour.
type Color uint8

const (
	Plain Color = iota
	Comment
	String
	Number
	Keyword
	Type
	Ident
	Special
	Preproc
	Symbol
	numColors
)

// Style bits of a Run. The heading levels are exclusive of each other.
const (
	Bold      = 1 << iota // **strong**
	Italic                // *emphasis*
	Strike                // ~~strike~~
	Code                  // inline code, code blocks, tables: monospace
	Link                  // a link's text
	Quote                 // inside a blockquote
	CodeBlock             // a fenced or indented code block: shaded
	H1
	H2
	H3
	colorShift = 12 // the Color of a highlighted code block sits above the flags
)

// Run is a piece of text with one style.
type Run struct {
	Text  string
	Flags int
	Color Color
}

// Encode is the wire form the GUI parses: for each run, "flags:color", a unit
// separator, the text, and a record separator.
func Encode(runs []Run) string {
	var b []byte
	for _, r := range runs {
		b = appendInt(b, r.Flags|int(r.Color)<<colorShift)
		b = append(b, '\x1f')
		b = append(b, r.Text...)
		b = append(b, '\x1e')
	}
	return string(b)
}

func appendInt(b []byte, n int) []byte {
	if n == 0 {
		return append(b, '0')
	}
	var tmp [20]byte
	i := len(tmp)
	for n > 0 {
		i--
		tmp[i] = byte('0' + n%10)
		n /= 10
	}
	return append(b, tmp[i:]...)
}
