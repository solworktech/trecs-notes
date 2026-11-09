//go:build linux && !nogtk

package ui

/*
#cgo pkg-config: gtk+-3.0
#include <stdlib.h>
#include "gtk_linux.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

var settingsHandler SettingsHandler

//export goSettingsAction
func goSettingsAction(action C.int, a1, a2, a3 *C.char) *C.char {
	r := SettingsReply{Message: "no handler"}
	if settingsHandler != nil {
		r = settingsHandler(int(action), C.GoString(a1), C.GoString(a2), C.GoString(a3))
	}
	ok := 0
	if r.OK {
		ok = 1
	}
	return C.CString(fmt.Sprintf("%s\x1f%s\x1f%d\x1f%d", r.Message, r.LoginStatus, r.Unsynced, ok))
}

// ShowSettings opens the Settings window and blocks until it closes.
func ShowSettings(d SettingsData, h SettingsHandler) (SettingsResult, error) {
	settingsHandler = h
	var strs []unsafe.Pointer
	cs := func(s string) *C.char {
		p := C.CString(s)
		strs = append(strs, unsafe.Pointer(p))
		return p
	}
	defer func() {
		for _, p := range strs {
			C.free(p)
		}
	}()
	var icon unsafe.Pointer
	if len(iconPNG) > 0 {
		icon = C.CBytes(iconPNG)
		defer C.free(icon)
	}
	b2i := func(b bool) C.int {
		if b {
			return 1
		}
		return 0
	}
	in := C.settings_in{
		hotkey: cs(d.Hotkey), quick_hotkey: cs(d.QuickHotkey), width: C.int(d.Width), height: C.int(d.Height), remote: b2i(d.Remote), copy_fallback: b2i(d.CopyFallback),
		server: cs(d.Server), email: cs(d.Email), password: cs(d.Password), login_status: cs(d.LoginStatus),
		unsynced: C.int(d.Unsynced), db_path: cs(d.DBPath),
		icon: (*C.uchar)(icon), iconlen: C.int(len(iconPNG)),
	}
	var out C.settings_out
	switch C.trecs_settings(&in, &out) {
	case -1:
		return SettingsResult{}, errGTKUnavailable
	case 0:
		return SettingsResult{}, nil
	}
	defer C.g_free(C.gpointer(out.hotkey))
	defer C.g_free(C.gpointer(out.quick_hotkey))
	defer C.g_free(C.gpointer(out.server))
	defer C.g_free(C.gpointer(out.email))
	defer C.g_free(C.gpointer(out.password))
	return SettingsResult{
		Hotkey: C.GoString(out.hotkey), QuickHotkey: C.GoString(out.quick_hotkey), Width: int(out.width), Height: int(out.height),
		Remote: out.remote != 0, CopyFallback: out.copy_fallback != 0, Server: C.GoString(out.server), Email: C.GoString(out.email),
		Password: C.GoString(out.password), Saved: true,
	}, nil
}
