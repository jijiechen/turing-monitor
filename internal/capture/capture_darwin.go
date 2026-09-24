//go:build darwin

// Package capture turns a display into an H.264 Annex-B stream.
//
// On macOS this is ScreenCaptureKit feeding VideoToolbox, both hardware
// accelerated. On Linux it shells out to ffmpeg, which is the pragmatic choice
// there because it also handles the platform's display capture.
package capture

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework ScreenCaptureKit -framework VideoToolbox -framework CoreMedia -framework CoreVideo -framework CoreGraphics -framework AppKit

#include <stdint.h>
#include <stdlib.h>

// Implemented in capture_darwin.m.
int  turzx_capture_start(uint32_t displayID, int width, int height, int fps, int bitrate);
int  turzx_capture_read(uint8_t *dst, int cap);
int  turzx_capture_error(void);
void turzx_capture_stop(void);
*/
import "C"

import (
	"errors"
	"fmt"
	"time"
	"unsafe"
)

// Options configures a capture session.
type Options struct {
	// DisplayID is the CGDirectDisplayID to capture.
	DisplayID uint32
	// Width and Height are the capture resolution in pixels.
	Width, Height int
	// FPS is the requested frame rate.
	FPS int
	// Bitrate is the target H.264 bitrate in bits per second.
	Bitrate int
}

// frameBufferSize bounds a single encoded frame. Keyframes of a 720x1280
// desktop comfortably fit; anything larger is a sign of a misconfigured
// encoder and is dropped rather than streamed.
const frameBufferSize = 512 * 1024

// Session is a running capture. Frames are read with Next.
type Session struct {
	opts Options
	buf  []byte
}

// Start begins capturing. It blocks until the stream is running, so a
// permission problem surfaces here rather than as a silent black screen.
func Start(opts Options) (*Session, error) {
	if opts.Width <= 0 || opts.Height <= 0 {
		return nil, fmt.Errorf("capture: invalid size %dx%d", opts.Width, opts.Height)
	}
	if opts.FPS <= 0 {
		opts.FPS = 30
	}
	if opts.Bitrate <= 0 {
		opts.Bitrate = 4_000_000
	}

	rc := C.turzx_capture_start(C.uint32_t(opts.DisplayID), C.int(opts.Width),
		C.int(opts.Height), C.int(opts.FPS), C.int(opts.Bitrate))
	if rc != 0 {
		return nil, fmt.Errorf("capture: %s", describeError(int(rc)))
	}
	return &Session{opts: opts, buf: make([]byte, frameBufferSize)}, nil
}

// Next returns the next encoded frame, blocking until one is available.
//
// The returned slice is only valid until the following call.
func (s *Session) Next() ([]byte, error) {
	for {
		if err := s.checkError(); err != nil {
			return nil, err
		}
		n := int(C.turzx_capture_read((*C.uint8_t)(unsafe.Pointer(&s.buf[0])), C.int(len(s.buf))))
		switch {
		case n > 0:
			return s.buf[:n], nil
		case n == 0:
			// No frame ready yet; ScreenCaptureKit only emits on change, so
			// this is normal on a static desktop.
			time.Sleep(2 * time.Millisecond)
		default:
			return nil, fmt.Errorf("capture: read failed (code %d)", n)
		}
	}
}

// checkError surfaces an asynchronous capture failure.
func (s *Session) checkError() error {
	if rc := int(C.turzx_capture_error()); rc != 0 {
		return fmt.Errorf("capture: %s", describeError(rc))
	}
	return nil
}

// Close stops capture and releases the encoder.
func (s *Session) Close() error {
	C.turzx_capture_stop()
	return nil
}

// describeError turns the Objective-C layer's status codes into something a
// user can act on.
func describeError(code int) string {
	switch code {
	case 1:
		return "the display was not visible to ScreenCaptureKit " +
			"(the virtual display must be created before capture starts)"
	case 2:
		return "Screen Recording permission is not granted for this program — " +
			"enable it in System Settings > Privacy & Security > Screen Recording, " +
			"then restart the terminal"
	case 3:
		return "the capture stream failed to start"
	case 4:
		return "the H.264 encoder could not be created"
	case 5:
		return "the capture stream did not start in time"
	default:
		return fmt.Sprintf("unknown error %d", code)
	}
}

// ErrUnsupported reports a platform without a capture backend.
var ErrUnsupported = errors.New("capture: unsupported platform")
