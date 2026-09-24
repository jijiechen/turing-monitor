// Package vdisplay creates a real virtual display so the panel can act as an
// extended desktop rather than just a picture frame.
//
// Neither platform makes this easy, and they fail in opposite directions:
//
//   - macOS has no public API for adding a display at all. It uses the private
//     CGVirtualDisplay classes inside CoreGraphics, the same route DisplayLink,
//     BetterDisplay and DeskPad take. The program can create the display
//     itself, but the API is undocumented and may break in any OS update.
//   - Linux has a supported mechanism (evdi, or the X11 dummy driver) but it
//     needs a kernel module and system configuration, so a userspace program
//     cannot conjure a display on its own. It can only find one that has
//     already been configured and capture it.
package vdisplay

import (
	"fmt"

	"github.com/jijiechen/turing-monitor/internal/framesrc"
)

// Options describes the virtual display to create or find.
type Options struct {
	// Name is what the display is called, on platforms that let it be chosen.
	Name string
	// Output selects a specific output on platforms where the display has to
	// already exist. Empty means auto-detect.
	Output string
	// Width and Height are the desired pixel dimensions, normally the panel's
	// own resolution.
	Width, Height int
	// RefreshHz is the requested refresh rate. Zero means 60.
	RefreshHz int
	// WidthMM and HeightMM are the physical size to report in the EDID. They
	// matter because a compositor derives a display's scale factor from the
	// ratio of pixel size to physical size; see BuildEDID.
	WidthMM, HeightMM int
}

// normalise fills in defaults and rejects impossible geometry.
func (o Options) normalise() (Options, error) {
	if o.Width <= 0 || o.Height <= 0 {
		return o, fmt.Errorf("vdisplay: invalid size %dx%d", o.Width, o.Height)
	}
	if o.RefreshHz <= 0 {
		o.RefreshHz = 60
	}
	if o.Name == "" {
		o.Name = "TURZX"
	}
	if o.WidthMM <= 0 || o.HeightMM <= 0 {
		// Report about 100 DPI. A realistic size for a small panel would make
		// a compositor treat it as high density and hand back a cramped
		// logical desktop, which is the same trap the macOS side hit.
		const logicalDPI = 100.0
		o.WidthMM = int(float64(o.Width) / logicalDPI * 25.4)
		o.HeightMM = int(float64(o.Height) / logicalDPI * 25.4)
	}
	return o, nil
}

// Display is a virtual display that frames can be captured from.
type Display struct {
	// id is the platform display identifier: a CGDirectDisplayID on macOS.
	id uint32
	// name is the platform output name, e.g. an xrandr output on Linux.
	name string
	// x and y are the display's origin within the platform's coordinate space.
	// On Linux, capture works from screen coordinates, so the origin matters;
	// on macOS the display id is sufficient.
	x, y   int
	width  int
	height int

	// source is set when the display also produces its own pixels, as evdi
	// does on Linux: the kernel module creates a real DRM device and the
	// library hands back its framebuffer. Callers that capture should prefer
	// it, since it needs no screenshot and no X server.
	source framesrc.Source

	// sourceErr records why a pixel-producing display was unavailable, so a
	// caller falling back to screen capture can explain what it lost.
	sourceErr error
}

// Source returns a pixel source when the display provides its own, or nil when
// the caller has to capture the screen instead.
func (d *Display) Source() framesrc.Source { return d.source }

// SourceError returns the reason a pixel-producing display was unavailable, or
// nil if one was found.
func (d *Display) SourceError() error { return d.sourceErr }

// ID returns the platform display identifier.
func (d *Display) ID() uint32 { return d.id }

// Name returns the platform output name, when the platform has one.
func (d *Display) Name() string { return d.name }

// Size returns the display's pixel dimensions.
func (d *Display) Size() (int, int) { return d.width, d.height }

// Origin returns the display's top-left corner in screen coordinates. It is
// only meaningful where capture is addressed by region rather than by id.
func (d *Display) Origin() (int, int) { return d.x, d.y }

// UnsupportedError describes a platform or configuration where a virtual
// display cannot be created, with enough detail to act on.
type UnsupportedError struct {
	Platform string
	Detail   string
	Hint     string
}

func (e *UnsupportedError) Error() string {
	msg := fmt.Sprintf("virtual display is not available on %s", e.Platform)
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	if e.Hint != "" {
		msg += "\n  hint: " + e.Hint
	}
	return msg
}
