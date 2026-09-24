package usb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// intervals controls how often the supervisor polls and how often it reminds
// the user what it is waiting for. They are a struct so tests can shorten them.
type intervals struct {
	absent time.Duration // between attempts to find an absent panel
	retry  time.Duration // between attempts after a session fails
	remind time.Duration // between "still waiting" messages
}

var defaultIntervals = intervals{
	absent: time.Second,
	retry:  time.Second,
	remind: 60 * time.Second,
}

// Supervise keeps a panel session alive across unplugging and replugging.
//
// A display is expected to keep working when it is unplugged and plugged back
// in, without anyone restarting anything, so this never gives up: it waits for
// a panel to appear, hands it to fn, and when fn returns it closes the device
// and waits for one to appear again.
//
// fn runs until it fails or ctx is cancelled. It must not hold on to the
// device after returning. On the first call the panel is usually already
// present; later calls follow reconnection, and the panel will have reset
// itself, so anything the device needs before it accepts data has to be set up
// again inside fn rather than once outside it.
//
// Supervise returns nil when ctx is cancelled, and only returns an error for
// something it cannot recover from.
func Supervise(ctx context.Context, log io.Writer, fn func(*Device) error) error {
	return supervise(ctx, log, Open, fn, defaultIntervals)
}

// supervise is Supervise with its device opener and timings injected, so the
// retry behaviour can be tested without hardware.
func supervise(ctx context.Context, log io.Writer, open func() (*Device, error),
	fn func(*Device) error, iv intervals) error {

	first := true
	lastReminder := time.Time{}

	for {
		if ctx.Err() != nil {
			return nil
		}

		dev, err := open()
		if err != nil {
			// A missing panel is the expected case, not a failure worth
			// repeating on every poll.
			if !errors.Is(err, ErrNotFound) {
				fmt.Fprintf(log, "cannot open display: %v\n", err)
			}
			if first || time.Since(lastReminder) >= iv.remind {
				fmt.Fprintf(log, "waiting for display...\n")
				lastReminder = time.Now()
			}
			first = false
			if !sleep(ctx, iv.absent) {
				return nil
			}
			continue
		}

		if first {
			fmt.Fprintf(log, "connected to %s\n", dev.Model())
		} else {
			fmt.Fprintf(log, "display reconnected: %s\n", dev.Model())
		}
		first = false

		runErr := fn(dev)
		dev.Close()

		if ctx.Err() != nil {
			return nil
		}
		if runErr != nil {
			fmt.Fprintf(log, "display session ended: %v\n", runErr)
		} else {
			fmt.Fprintf(log, "display session ended\n")
		}
		fmt.Fprintf(log, "waiting for display...\n")
		lastReminder = time.Now()

		if !sleep(ctx, iv.retry) {
			return nil
		}
	}
}

// sleep waits for d, returning false if ctx was cancelled first.
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
