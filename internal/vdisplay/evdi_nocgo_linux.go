//go:build linux && !cgo

package vdisplay

import (
	"errors"

	"github.com/jijiechen/turing-monitor/internal/framesrc"
)

// openEVDI reports that evdi support was not compiled in.
//
// The bindings need cgo, because they call into libevdi. Without cgo the file
// that defines them is excluded, so this stands in for it and the X11 path is
// used instead.
func openEVDI(opts Options) (framesrc.Source, error) {
	return nil, errors.New("this build has no cgo, so evdi support is not compiled in")
}
