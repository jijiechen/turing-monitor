//go:build !darwin && !linux

package vdisplay

import "runtime"

// Create reports that virtual displays are not supported here.
//
// Only macOS and Linux have backends. Windows is deliberately excluded: it has
// a supported mechanism (IddCx), but implementing it means shipping a signed
// kernel-mode driver, which is a different project from this one.
func Create(opts Options) (*Display, error) {
	return nil, &UnsupportedError{
		Platform: runtime.GOOS,
		Detail:   "only macOS and Linux are supported",
	}
}

// Close is a no-op where nothing can be created.
func (d *Display) Close() error { return nil }
