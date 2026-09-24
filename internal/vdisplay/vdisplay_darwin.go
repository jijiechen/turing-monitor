//go:build darwin

package vdisplay

/*
#cgo CFLAGS: -fobjc-arc
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
	"time"
	"unsafe"
)

// Create adds a virtual display and returns it.
//
// The caller must keep the process alive and call Close when finished; macOS
// removes the display as soon as the owning process exits.
func Create(opts Options) (*Display, error) {
	opts, err := opts.normalise()
	if err != nil {
		return nil, err
	}

	cName := C.CString(opts.Name)
	defer C.free(unsafe.Pointer(cName))

	var id C.uint32_t
	rc := C.turzx_vd_create(cName, C.int(opts.Width), C.int(opts.Height), C.int(opts.RefreshHz), &id)
	if rc != 0 {
		return nil, fmt.Errorf("create virtual display: %s", createError(rc))
	}
	return &Display{
		id:     uint32(id),
		name:   opts.Name,
		width:  opts.Width,
		height: opts.Height,
	}, nil
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

// Close removes the virtual display.
func (d *Display) Close() error {
	C.turzx_vd_destroy()
	return nil
}

// SettleDelay is how long to wait after creating the display before capturing
// it. macOS publishes a new display asynchronously, so capture started too early finds nothing.
const SettleDelay = 1500 * time.Millisecond
