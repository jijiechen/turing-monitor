package usb

import (
	"testing"
	"time"
)

// TestFlushTimeoutStaysCheap guards the streaming frame budget.
//
// Every send drains the IN endpoint afterwards, and the device almost always
// has nothing extra queued, so the drain pays its timeout in full on every
// frame. At 100ms that capped streaming at about 7.5 fps -- nowhere near what
// the USB link or the encoder could sustain, just dead waiting. Nothing in the
// protocol or the hardware was the limit, so no amount of bitrate tuning would
// have fixed it.
func TestFlushTimeoutStaysCheap(t *testing.T) {
	const frameBudget = time.Second / 30

	if flushTimeout >= frameBudget/4 {
		t.Errorf("flushTimeout is %v; one drain must cost far less than a 30fps frame budget (%v)",
			flushTimeout, frameBudget)
	}
}

// TestTimeoutOrdering checks the timeouts make sense relative to each other: a
// real reply must be given longer than a speculative drain, and a bulk write
// carrying an image must be given longer still.
func TestTimeoutOrdering(t *testing.T) {
	if readTimeout <= flushTimeout {
		t.Errorf("readTimeout (%v) must exceed flushTimeout (%v), or real replies get cut off",
			readTimeout, flushTimeout)
	}
	if writeTimeout <= readTimeout {
		t.Errorf("writeTimeout (%v) must exceed readTimeout (%v), or large payload writes time out",
			writeTimeout, readTimeout)
	}
}

func TestFlushAttemptsBounded(t *testing.T) {
	// The drain must stay bounded: it runs on every send, so an unbounded loop
	// would stall the pipeline whenever the device chattered.
	if flushAttempts <= 0 || flushAttempts > 10 {
		t.Errorf("flushAttempts = %d, want 1-10", flushAttempts)
	}
}
