//go:build linux && !nogtk

package ui

/*
#cgo pkg-config: gtk+-3.0
#include <stdlib.h>
#include "gtk_linux.h"
*/
import "C"

import (
	"unsafe"
)

func runGTK(in Input) (Result, error) {
	app, title, text := C.CString(in.Snapshot.AppName), C.CString(in.Snapshot.WindowTitle), C.CString(in.Snapshot.SelectedText)
	defer C.free(unsafe.Pointer(app))
	defer C.free(unsafe.Pointer(title))
	defer C.free(unsafe.Pointer(text))

	n := len(in.TagSuggestions)
	ptrs := C.malloc(C.size_t(n+1) * C.size_t(unsafe.Sizeof(uintptr(0))))
	defer C.free(ptrs)
	arr := unsafe.Slice((**C.char)(ptrs), n+1)
	for i, s := range in.TagSuggestions {
		arr[i] = C.CString(s)
		defer C.free(unsafe.Pointer(arr[i]))
	}

	var icon unsafe.Pointer
	if len(iconPNG) > 0 {
		icon = C.CBytes(iconPNG)
		defer C.free(icon)
	}
	var oApp, oTitle, oTags, oText *C.char
	switch C.trecs_form(app, title, text, (**C.char)(ptrs), C.int(n), C.int(in.Width), C.int(in.Height), (*C.uchar)(icon), C.int(len(iconPNG)), &oApp, &oTitle, &oTags, &oText) {
	case -1:
		return Result{}, errGTKUnavailable
	case 0:
		return Result{Confirmed: false}, nil
	}
	defer C.g_free(C.gpointer(oApp))
	defer C.g_free(C.gpointer(oTitle))
	defer C.g_free(C.gpointer(oTags))
	defer C.g_free(C.gpointer(oText))
	return Result{
		AppName: C.GoString(oApp), WindowTitle: C.GoString(oTitle),
		TagsRaw: C.GoString(oTags), Text: C.GoString(oText), Confirmed: true,
	}, nil
}
