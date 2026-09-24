package usb

import (
	"context"
	"fmt"
	"time"

	"github.com/jijiechen/turing-monitor/internal/proto"
)

const (
	// writeTimeout bounds a single bulk write. Large image uploads over a
	// 480 Mbit/s link need a few hundred milliseconds at most.
	writeTimeout = 5 * time.Second
	// readTimeout bounds the reply read. The device answers promptly.
	readTimeout = 2 * time.Second
	// flushTimeout is used to drain stale replies from the IN endpoint.
	flushTimeout = 100 * time.Millisecond
	// flushAttempts caps how many stale replies we drain at once.
	flushAttempts = 5
)

// Send writes payload to the bulk OUT endpoint and returns the device's reply.
// A nil reply with a nil error means the device acknowledged by saying nothing.
func (d *Device) Send(payload []byte) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sendLocked(payload)
}

func (d *Device) sendLocked(payload []byte) ([]byte, error) {
	wctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	n, err := d.out.WriteContext(wctx, payload)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("usb write: %w", err)
	}
	if n != len(payload) {
		return nil, fmt.Errorf("usb write: short transfer, %d of %d bytes", n, len(payload))
	}
	return d.readReplyLocked()
}

// SendAck writes payload and requires the device to acknowledge it.
func (d *Device) SendAck(payload []byte) error {
	resp, err := d.Send(payload)
	if err != nil {
		return err
	}
	if !proto.IsAck(resp) {
		return fmt.Errorf("device did not acknowledge (reply % x)", resp)
	}
	return nil
}

// readReplyLocked reads one reply block, then drains any stale replies so the
// next command starts with a clean IN endpoint.
func (d *Device) readReplyLocked() ([]byte, error) {
	buf := make([]byte, ReadSize)
	rctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	n, err := d.in.ReadContext(rctx, buf)
	cancel()
	if err != nil {
		// A missing reply is not fatal: the device does not answer every
		// command. Report it as an empty reply so callers can decide.
		return nil, nil
	}
	reply := buf[:n]
	d.flushLocked()
	return reply, nil
}

// flushLocked drains leftover replies from the IN endpoint.
func (d *Device) flushLocked() {
	buf := make([]byte, ReadSize)
	for i := 0; i < flushAttempts; i++ {
		fctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
		_, err := d.in.ReadContext(fctx, buf)
		cancel()
		if err != nil {
			return
		}
	}
}
