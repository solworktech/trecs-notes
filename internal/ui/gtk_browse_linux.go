//go:build linux && !nogtk

package ui

/*
#cgo pkg-config: gtk+-3.0
#include <stdlib.h>
#include "gtk_linux.h"
*/
import "C"

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"unsafe"
)

// Ops understood by goBrowseAction; keep in step with gtk_browse.c.
const (
	bSearch = 1
	bGet    = 2
	bSave   = 3
	bDelete = 4
	bSync   = 5
	bStatus = 6
	bPoll   = 7
	bTags   = 8
)

var (
	browseHandler BrowseHandler
	searchMu      sync.Mutex
	searchCancel  context.CancelFunc
)

// field makes text safe for the separators the window splits on.
func field(s string) string {
	return strings.NewReplacer("\x1e", " ", "\x1f", " ", "\x00", "").Replace(s)
}

func flag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func reply(ok bool, msg string) *C.char { return C.CString(flag(ok) + "\x1f" + field(msg)) }

//export goBrowseAction
func goBrowseAction(op C.int, a1, a2, a3, a4, a5 *C.char) *C.char {
	h := browseHandler
	switch int(op) {
	case bSearch:
		// a1 text, a2 regex, a3 match case, a4 generation
		gen, _ := strconv.Atoi(C.GoString(a4))
		text, regex, mcase := C.GoString(a1), C.GoString(a2) == "1", C.GoString(a3) == "1"
		ctx, cancel := context.WithCancel(context.Background())
		searchMu.Lock()
		if searchCancel != nil {
			searchCancel() // its words are no longer in the box
		}
		searchCancel = cancel
		searchMu.Unlock()
		go runSearch(ctx, cancel, h, gen, text, regex, mcase)
		return C.CString("")

	case bGet:
		id, _ := strconv.ParseInt(C.GoString(a1), 10, 64)
		n, err := h.Get(id)
		if err != nil {
			return C.CString("\x1e" + err.Error())
		}
		renderTitleHint = n.Title // what Detect reads the language from
		// the text comes last: the window splits into seven, so it may hold any character
		return C.CString(strings.Join([]string{field(n.App), field(n.Title), field(n.Tags), field(n.Created), field(n.Sync), flag(n.Markdown), strings.ReplaceAll(n.Text, "\x00", "")}, "\x1f"))

	case bSave:
		// a1 id, a2 "app \x1f title \x1f tags", a3 text
		id, _ := strconv.ParseInt(C.GoString(a1), 10, 64)
		parts := strings.SplitN(C.GoString(a2), "\x1f", 3)
		for len(parts) < 3 {
			parts = append(parts, "")
		}
		msg, err := h.Save(BrowseNote{ID: id, App: parts[0], Title: parts[1], Tags: parts[2], Text: C.GoString(a3)})
		if err != nil {
			return reply(false, err.Error())
		}
		return reply(true, msg)

	case bDelete:
		id, _ := strconv.ParseInt(C.GoString(a1), 10, 64)
		if err := h.Delete(id); err != nil {
			return reply(false, err.Error())
		}
		return reply(true, "Note deleted.")

	case bSync:
		msg, ok := h.Sync()
		return reply(ok, msg)

	case bStatus:
		text, attention := h.Status()
		return C.CString(flag(attention) + "\x1f" + flag(h.SyncOn()) + "\x1f" + field(text))

	case bPoll:
		return C.CString(flag(h.Changed()))

	case bTags:
		tags := h.Tags()
		for i := range tags {
			tags[i] = field(tags[i])
		}
		return C.CString(strings.Join(tags, "\x1f"))
	}
	return C.CString("")
}

// runSearch streams a search to the window in batches, from this goroutine;
// trecs_browse_deliver hands each to the window's thread.
func runSearch(ctx context.Context, cancel context.CancelFunc, h BrowseHandler, gen int, text string, regex, mcase bool) {
	defer cancel()
	deliver := func(payload string, kind int) {
		if ctx.Err() != nil {
			return
		}
		cp := C.CString(payload)
		C.trecs_browse_deliver(C.int(gen), cp, C.int(kind))
		C.free(unsafe.Pointer(cp))
	}
	var batch []string
	flush := func() {
		if len(batch) > 0 {
			deliver(strings.Join(batch, "\x1e"), 0)
			batch = batch[:0]
		}
	}
	first := true
	capped, problem := h.Search(ctx, text, regex, mcase, func(r BrowseRow) bool {
		batch = append(batch, strings.Join([]string{strconv.FormatInt(r.ID, 10), field(r.Mark), field(r.Date), field(r.App), field(r.Snippet)}, "\x1f"))
		// the first rows go at once, so something shows; then in pages
		if first || len(batch) >= 200 {
			first = false
			flush()
		}
		return ctx.Err() == nil
	})
	if ctx.Err() != nil {
		return // a newer search has taken over
	}
	flush()
	deliver(flag(capped)+"\x1f"+field(problem), 1)
}

// ShowBrowse opens the browse window and blocks until it closes.
func ShowBrowse(sz BrowseSize, h BrowseHandler) error {
	browseHandler = h
	var icon unsafe.Pointer
	if len(iconPNG) > 0 {
		icon = C.CBytes(iconPNG)
		defer C.free(icon)
	}
	in := C.browse_in{icon: (*C.uchar)(icon), iconlen: C.int(len(iconPNG)), width: C.int(sz.Width), height: C.int(sz.Height)}
	if C.trecs_browse(&in) == -1 {
		return errGTKUnavailable
	}
	return nil
}
