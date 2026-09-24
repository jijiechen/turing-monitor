package usb

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testIntervals keeps the tests quick while exercising the same code paths.
var testIntervals = intervals{
	absent: 2 * time.Millisecond,
	retry:  2 * time.Millisecond,
	remind: 5 * time.Millisecond,
}

// opener returns an open function that reports a panel as absent the given
// number of times before succeeding.
func opener(absentTimes int, opened *int32) func() (*Device, error) {
	var calls int32
	return func() (*Device, error) {
		n := atomic.AddInt32(&calls, 1)
		if int(n) <= absentTimes {
			return nil, ErrNotFound
		}
		if opened != nil {
			atomic.AddInt32(opened, 1)
		}
		return &Device{}, nil
	}
}

// TestSuperviseWaitsForAbsentPanel covers the case a service hits at boot: the
// panel is not plugged in yet, and the program must wait rather than exit.
func TestSuperviseWaitsForAbsentPanel(t *testing.T) {
	var opened int32
	var log bytes.Buffer

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var sessions int32
	done := make(chan error, 1)
	go func() {
		done <- supervise(ctx, &log, opener(2, &opened), func(*Device) error {
			if atomic.AddInt32(&sessions, 1) == 1 {
				// One session is enough; stop the supervisor from the inside.
				cancel()
			}
			return nil
		}, testIntervals)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("supervise returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("supervise did not return after cancellation")
	}

	if got := atomic.LoadInt32(&opened); got != 1 {
		t.Errorf("opened %d times, want 1", got)
	}
	if !strings.Contains(log.String(), "waiting for display") {
		t.Errorf("log does not mention waiting:\n%s", log.String())
	}
}

// TestSuperviseReopensAfterFailure is the unplug/replug case: a session ends
// with an error, and the supervisor must try again rather than give up.
func TestSuperviseReopensAfterFailure(t *testing.T) {
	var opened int32
	var log bytes.Buffer

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var sessions int32
	done := make(chan error, 1)
	go func() {
		done <- supervise(ctx, &log, opener(0, &opened), func(*Device) error {
			if atomic.AddInt32(&sessions, 1) >= 3 {
				cancel()
				return nil
			}
			return errors.New("device went away")
		}, testIntervals)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("supervise did not finish")
	}

	if got := atomic.LoadInt32(&opened); got != 3 {
		t.Errorf("opened %d times, want 3 (one per attempt)", got)
	}
	if n := strings.Count(log.String(), "display session ended: device went away"); n != 2 {
		t.Errorf("logged the failure %d times, want 2\n%s", n, log.String())
	}
	// A reconnect must be announced, since the panel will have reset.
	if !strings.Contains(log.String(), "display reconnected") {
		t.Errorf("log does not report reconnection:\n%s", log.String())
	}
}

// TestSuperviseDoesNotSpamWhileAbsent checks the waiting message is tied to the
// reminder interval rather than the poll interval. A service waiting days for a
// panel must not write a line every second.
func TestSuperviseDoesNotSpamWhileAbsent(t *testing.T) {
	const (
		run    = 200 * time.Millisecond
		absent = 2 * time.Millisecond
		remind = 40 * time.Millisecond
	)
	iv := intervals{absent: absent, retry: absent, remind: remind}

	var log bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		time.Sleep(run)
		cancel()
	}()
	_ = supervise(ctx, &log, opener(1<<30, nil), func(*Device) error { return nil }, iv)

	got := strings.Count(log.String(), "waiting for display")
	// Polling would give around run/absent = 100 messages; reminding should
	// give around run/remind = 5.
	if maxExpected := int(run/remind) + 3; got > maxExpected {
		t.Errorf("logged the wait %d times; expected about %d, tied to the reminder interval\n%s",
			got, int(run/remind), log.String())
	}
	if got == 0 {
		t.Error("never said it was waiting, so a silent service would look hung")
	}
}

// TestSuperviseStopsPromptlyWhenCancelled checks cancellation during the wait
// for a panel returns quickly rather than after the poll interval.
func TestSuperviseStopsPromptlyWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	slow := intervals{absent: time.Hour, retry: time.Hour, remind: time.Hour}

	done := make(chan struct{})
	go func() {
		_ = supervise(ctx, &bytes.Buffer{}, opener(1<<30, nil), func(*Device) error { return nil }, slow)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("did not stop within a second of cancellation")
	}
}

// TestSuperviseClosesDeviceBetweenSessions checks each session gets its own
// device handle and that a failed session does not leak it.
func TestSuperviseClosesDeviceBetweenSessions(t *testing.T) {
	var log bytes.Buffer
	var seen int32

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	open := func() (*Device, error) {
		atomic.AddInt32(&seen, 1)
		return &Device{}, nil
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	_ = supervise(ctx, &log, open, func(d *Device) error {
		if d == nil {
			t.Error("fn received a nil device")
		}
		time.Sleep(5 * time.Millisecond)
		return errors.New("stop")
	}, testIntervals)

	if atomic.LoadInt32(&seen) < 2 {
		t.Errorf("only %d sessions ran; each failure should lead to another attempt", seen)
	}
}
