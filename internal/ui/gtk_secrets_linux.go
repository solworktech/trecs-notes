//go:build linux && !nogtk

package ui

/*
#cgo pkg-config: gtk+-3.0
#include <stdlib.h>
#include "gtk_linux.h"
*/
import "C"

import "unsafe"

// runGTKSecrets shows the question as a GTK message dialog.
func runGTKSecrets(msg string) (SecretChoice, error) {
	cmsg := C.CString(msg)
	defer C.free(unsafe.Pointer(cmsg))
	switch C.trecs_secrets(cmsg) {
	case -1:
		return SecretCancel, errGTKUnavailable
	case 1:
		return SecretMask, nil
	case 2:
		return SecretKeep, nil
	}
	return SecretCancel, nil
}
