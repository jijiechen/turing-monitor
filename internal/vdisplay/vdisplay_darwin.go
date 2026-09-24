//go:build darwin

// Package vdisplay creates a real virtual display so the panel can act as an
// extended desktop rather than just a picture frame.
//
// The mechanisms differ per platform and both are unusual:
//
//   - macOS has no public API for adding a display. It uses the private
//     CGVirtualDisplay classes inside CoreGraphics, the same route DisplayLink,
//     BetterDisplay and DeskPad take. See vdisplay_darwin.m.
//   - Linux uses an X11 virtual output (evdi or the dummy driver) and captures
//     it with a helper process. See vdisplay_linux.go.
package vdisplay

/*
#cgo LDFLAGS: -framework Foundation -framework CoreGraphics -framework AppKit

#include <stdint.h>
#include <stdlib.h>

// Implemented in vdisplay_darwin.m.
int turzx_vd_create(const char *name, int width, int height, int refreshHz, uint32_t *outID);
void turzx_vd_destroy(void);
uint32_t turzx_vd_display_id(void);
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Display is a created virtual display.
type Display struct {
	id     uint32
	width  int
	height int
}

// Create adds a virtual display of the given pixel size and returns it.
//
// The caller must keep the process alive and call Close when finished; macOS
// removes the display as soon as the owning process exits.
func Create(name string, width, height, refreshHz int) (*Display, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	var id C.uint32_t
	if rc := C.turzx_vd_create(cName, C.int(width), C.int(height), C.int(refreshHz), &id); rc != 0 {
		return nil, fmt.Errorf("create virtual display: %s", createError(rc))
	}
	return &Display{id: uint32(id), width: width, height: height}, nil
}

// createError explains the failure codes returned from Objective-C, which
// cannot return a Go error.
func createError(rc C.int) string {
	switch int(rc) {
	case -1:
		return "invalid dimensions"
	case -2:
		return "WindowServer refused the display descriptor " +
			"(this is private API and may have changed in this macOS release)"
	case -3:
		return "the display rejected the requested mode"
	case -4:
		return "CGVirtualDisplay is not present in this macOS release"
	default:
		return fmt.Sprintf("unknown error %d", int(rc))
	}
}

// ID returns the CGDirectDisplayID, which is what capture APIs expect.
func (d *Display) ID() uint32 { return d.id }

// Size returns the display's pixel dimensions.
func (d *Display) Size() (int, int) { return d.width, d.height }

// Close removes the virtual display.
func (d *Display) Close() error {
	C.turzx_vd_destroy()
	return nil
}
