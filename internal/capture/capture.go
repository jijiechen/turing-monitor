// Package capture turns a display into an H.264 Annex-B stream.
//
// Both platforms hand back the same thing -- a continuous Annex-B NAL stream --
// but get there differently. macOS captures a specific display through
// ScreenCaptureKit and encodes with VideoToolbox; Linux captures a screen
// region with ffmpeg, which also does the encoding.
package capture

import "errors"

// Options configures a capture session.
type Options struct {
	// DisplayID is the platform display identifier: a CGDirectDisplayID on
	// macOS. It is ignored where capture is addressed by region.
	DisplayID uint32
	// X and Y are the display's origin in screen coordinates, used where
	// capture is addressed by region (Linux with ffmpeg's x11grab).
	X, Y int
	// Width and Height are the capture resolution in pixels, normally the
	// panel's native size.
	Width, Height int
	// FPS is the requested frame rate. Zero means 30.
	FPS int
	// Bitrate is the target H.264 bitrate in bits per second. Zero means a
	// default suitable for desktop content.
	Bitrate int
}

// defaultBitrate targets desktop content at the panel's resolution. Text and
// window chrome need far more bits than video-rate defaults assume, and USB 2.0
// has the headroom to carry it.
const defaultBitrate = 16_000_000

// defaultFPS is used when the caller does not specify a frame rate.
const defaultFPS = 30

// normalise fills in defaults and rejects impossible geometry.
func (o Options) normalise() (Options, error) {
	if o.Width <= 0 || o.Height <= 0 {
		return o, errors.New("capture: width and height must be positive")
	}
	if o.FPS <= 0 {
		o.FPS = defaultFPS
	}
	if o.Bitrate <= 0 {
		o.Bitrate = defaultBitrate
	}
	return o, nil
}

// ErrUnsupported reports a platform without a capture backend.
var ErrUnsupported = errors.New("capture: unsupported platform")
