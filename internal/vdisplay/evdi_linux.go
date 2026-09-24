//go:build linux

package vdisplay

/*
#cgo LDFLAGS: -levdi
#include <stdlib.h>
#include "evdi_shim.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/jijiechen/turing-monitor/internal/framesrc"
)

// evdiConnectTimeout bounds the wait for the kernel to report a mode after
// connecting. Without a mode there is no size to allocate a framebuffer for.
const evdiConnectTimeout = 5 * time.Second

// evdiEventPoll is how long to block waiting for kernel events before looping.
// Long enough not to spin, short enough that a reconnect is noticed promptly.
const evdiEventPoll = 100

// activeSource lets the C callbacks find the source. libevdi hands the
// handlers only a void* we set, and passing a Go pointer through C is
// restricted, so the lookup goes through a package variable instead.
var activeSource atomic.Pointer[evdiSource]

// evdiSource is a display backed by a libevdi device. It is both the virtual
// display and the source of its pixels, which is why it satisfies
// framesrc.Source rather than only describing a display.
type evdiSource struct {
	handle C.evdi_handle

	mu     sync.Mutex
	width  int
	height int
	format framesrc.Format
	stride int

	// pixels is the framebuffer registered with the kernel. It is allocated
	// with C.malloc rather than as a Go slice because libevdi keeps the pointer
	// and writes into it asynchronously, which Go's pointer rules forbid for
	// memory Go owns.
	pixels  unsafe.Pointer
	bufSize int
	bufID   C.int

	// frameReady carries one token per grabbed frame, so a slow consumer drops
	// frames instead of letting a backlog build.
	frameReady chan struct{}

	// out is reused for every frame, since NextFrame's contract is that its
	// result is only valid until the next call.
	out []byte

	connected chan struct{}

	closeOnce sync.Once
	closed    chan struct{}
	loopDone  chan struct{}
}

// openEVDI creates a virtual display through libevdi.
//
// This requires root: adding a device means asking the kernel for a new DRM
// node and triggering a hotplug, which is privileged.
func openEVDI(opts Options) (framesrc.Source, error) {
	s := &evdiSource{
		format:     framesrc.FormatUnknown,
		frameReady: make(chan struct{}, 1),
		connected:  make(chan struct{}),
		closed:     make(chan struct{}),
		loopDone:   make(chan struct{}),
	}

	// Ask for a new device, then find it. libevdi does not report which index
	// it created, so scan for the highest one that is available.
	if rc := C.evdi_add_device(); rc != 0 {
		return nil, fmt.Errorf("evdi_add_device returned %d; the evdi kernel module may not be loaded "+
			"(try: sudo modprobe evdi)", int(rc))
	}

	index, err := findEVDIDevice()
	if err != nil {
		return nil, err
	}
	s.handle = C.evdi_open(C.int(index))
	if s.handle == nil {
		return nil, fmt.Errorf("evdi_open(%d) failed", index)
	}

	activeSource.Store(s)

	edid := BuildEDID(opts.Width, opts.Height, opts.WidthMM, opts.HeightMM, opts.Name)
	cEDID := C.CBytes(edid)
	defer C.free(cEDID)
	C.evdi_connect(s.handle, (*C.uchar)(cEDID), C.uint(len(edid)), 0)

	// The mode arrives asynchronously as an event, and it is the authority on
	// the framebuffer's size and pixel layout.
	select {
	case <-s.connected:
	case <-time.After(evdiConnectTimeout):
		s.Close()
		return nil, errors.New("evdi did not report a mode within " + evdiConnectTimeout.String() +
			"; the display may not have been picked up by a compositor")
	}

	if err := s.allocate(); err != nil {
		s.Close()
		return nil, err
	}

	go s.eventLoop()
	return s, nil
}

// findEVDIDevice returns the index of an available evdi device.
//
// The scan is bounded: evdi devices are appended, so the newest is the highest
// index, and probing far beyond the plausible count would only be slow.
func findEVDIDevice() (int, error) {
	const maxDevices = 32
	found := -1
	for i := 0; i < maxDevices; i++ {
		if C.evdi_check_device(C.int(i)) == C.AVAILABLE {
			found = i
		}
	}
	if found < 0 {
		return 0, errors.New("no evdi device found after adding one; " +
			"check that the evdi kernel module is loaded and that /dev/dri is accessible")
	}
	return found, nil
}

// allocate creates and registers the framebuffer for the current mode.
func (s *evdiSource) allocate() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	bpp := s.format.BytesPerPixel()
	if bpp == 0 {
		return fmt.Errorf("evdi reported pixel format %#x, which this program does not understand",
			uint32(s.format))
	}
	if s.width <= 0 || s.height <= 0 {
		return fmt.Errorf("evdi reported an implausible mode %dx%d", s.width, s.height)
	}

	stride := s.width * bpp
	size := stride * s.height

	pixels := C.malloc(C.size_t(size))
	if pixels == nil {
		return fmt.Errorf("could not allocate a %d byte framebuffer", size)
	}

	buf := C.struct_evdi_buffer{
		id:     0,
		buffer: pixels,
		width:  C.int(s.width),
		height: C.int(s.height),
		stride: C.int(stride),
	}
	C.evdi_register_buffer(s.handle, buf)

	s.pixels = pixels
	s.stride = stride
	s.bufSize = size
	s.bufID = 0
	s.out = make([]byte, size)
	return nil
}

// eventLoop drives libevdi: it asks for updates, waits for kernel events,
// dispatches them, and grabs pixels when the kernel says a frame is ready.
func (s *evdiSource) eventLoop() {
	defer close(s.loopDone)

	var ctx C.struct_evdi_event_context
	C.turzx_evdi_fill_context(&ctx)

	for {
		select {
		case <-s.closed:
			return
		default:
		}

		C.evdi_request_update(s.handle, s.bufID)

		rc := C.turzx_evdi_wait(s.handle, evdiEventPoll)
		if rc > 0 {
			C.evdi_handle_events(s.handle, &ctx)
			continue
		}
		// rc == 0 is a timeout, which is normal. A negative value means the
		// wait itself failed, so sleep rather than spin.
		if rc < 0 {
			select {
			case <-s.closed:
				return
			case <-time.After(time.Millisecond):
			}
		}
	}
}

// onModeChanged records the mode the kernel reported.
func (s *evdiSource) onModeChanged(width, height, bpp int, format framesrc.Format) {
	s.mu.Lock()
	changed := s.width != width || s.height != height || s.format != format
	s.width, s.height, s.format = width, height, format
	first := s.pixels == nil
	s.mu.Unlock()

	if changed && !first {
		// A mode change invalidates the buffer's dimensions, so it has to be
		// rebuilt. Reported rather than silently ignored, since a wrong-sized
		// buffer produces a skewed picture rather than an obvious failure.
		s.reallocate()
	}
	if first {
		select {
		case <-s.connected:
		default:
			close(s.connected)
		}
	}
}

func (s *evdiSource) reallocate() {
	s.mu.Lock()
	if s.pixels != nil {
		C.evdi_unregister_buffer(s.handle, s.bufID)
		C.free(s.pixels)
		s.pixels = nil
	}
	s.mu.Unlock()

	if err := s.allocate(); err != nil {
		// Nothing useful to do from an event callback; the next mode change
		// will try again.
		return
	}
}

// onUpdateReady grabs a frame the kernel says is ready.
func (s *evdiSource) onUpdateReady(bufferID int) {
	if C.turzx_evdi_grab(s.handle) <= 0 {
		// Zero rectangles means nothing was copied, which libevdi also uses to
		// signal a mode change rather than an empty frame.
		return
	}
	select {
	case s.frameReady <- struct{}{}:
	default:
		// A frame is already waiting to be collected, so this one would only
		// add latency.
	}
}

// NextFrame returns the most recently grabbed frame.
func (s *evdiSource) NextFrame() ([]byte, int, int, error) {
	select {
	case <-s.closed:
		return nil, 0, 0, errors.New("evdi source is closed")
	case <-s.frameReady:
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pixels == nil {
		return nil, 0, 0, errors.New("evdi has no framebuffer yet")
	}
	n := copy(s.out, unsafe.Slice((*byte)(s.pixels), s.bufSize))
	return s.out[:n], s.width, s.height, nil
}

// Format reports the pixel layout the kernel is using.
func (s *evdiSource) Format() framesrc.Format {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.format
}

// Close tears the display down and frees the framebuffer.
func (s *evdiSource) Close() error {
	s.closeOnce.Do(func() {
		close(s.closed)
		if s.handle != nil {
			C.evdi_disconnect(s.handle)
			C.evdi_close(s.handle)
			s.handle = nil
		}
		activeSource.CompareAndSwap(s, nil)

		s.mu.Lock()
		if s.pixels != nil {
			C.free(s.pixels)
			s.pixels = nil
		}
		s.mu.Unlock()

		// The loop may be inside a blocking wait, so it is not joined here;
		// the handle is already closed, which makes its next call fail.
	})
	return nil
}

// --- callbacks invoked from C -------------------------------------------------

//export turzxEvdiModeChanged
func turzxEvdiModeChanged(width, height, refresh, bpp C.int, format C.uint) {
	if s := activeSource.Load(); s != nil {
		s.onModeChanged(int(width), int(height), int(bpp), framesrc.Format(format))
	}
}

//export turzxEvdiUpdateReady
func turzxEvdiUpdateReady(bufferID C.int) {
	if s := activeSource.Load(); s != nil {
		s.onUpdateReady(int(bufferID))
	}
}

//export turzxEvdiDpms
func turzxEvdiDpms(mode C.int) {
	// The monitor being switched off is not actionable here: the panel is fed
	// from this process, and the compositor will simply stop producing frames.
}

// evdiVersion reports the library version, for diagnostics.
func evdiVersion() string {
	buf := make([]byte, 32)
	n := C.turzx_evdi_version((*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)))
	if n <= 0 {
		return "unknown"
	}
	return string(buf[:n])
}
