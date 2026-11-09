//go:build linux && !nogtk

package ui

/*
#cgo pkg-config: gtk+-3.0
#include <stdlib.h>
#include "gtk_linux.h"
*/
import "C"

import (
	"strings"

	"github.com/solworktech/trecs-notes/internal/render"
)

// Ops understood by goRender; keep in step with gtk_form.c.
const (
	opHighlight = 1
	opMarkdown  = 2
	opSyntaxes  = 3
	opFenceHint = 4
)

// renderTitleHint is the window title of the capture, used to guess the
// language of the note (a title such as "main.go - editor" says "go").
var renderTitleHint string

//export goRender
func goRender(op C.int, a, b *C.char) *C.char {
	text := C.GoString(a)
	switch int(op) {
	case opHighlight:
		// b: "" = auto-detect, "none" = no colours, otherwise a language
		syntax := C.GoString(b)
		resolved := syntax
		switch syntax {
		case "":
			resolved = render.Detect(renderTitleHint, text)
		case "none":
			resolved = ""
		}
		var spans string
		if resolved != "" {
			// a note wrapped in a fence is coloured inside it, so that the
			// fence itself does not open a string in the language
			body, skip := render.FenceBody(text)
			sp := render.Highlight(body, resolved)
			for i := range sp {
				sp[i].Start += skip
				sp[i].End += skip
			}
			spans = render.EncodeSpans(sp)
		}

		return C.CString(resolved + "\x1e" + spans)
	case opMarkdown:
		return C.CString(render.Encode(render.Markdown(text)))
	case opFenceHint:
		return C.CString(render.FenceHint(text))
	case opSyntaxes:
		return C.CString(strings.Join(render.Syntaxes(), "\x1f"))
	}
	return C.CString("")
}
