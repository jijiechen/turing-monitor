package dashboard

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"time"

	"github.com/jijiechen/turing-monitor/internal/metrics"
	"github.com/jijiechen/turing-monitor/internal/proto"
)

// Pusher is the part of the panel the dashboard needs. Keeping it narrow lets
// the renderer be tested without a device.
type Pusher interface {
	ShowJPEG(jpeg []byte) error
	Portrait() (int, int)
}

// Options configures a dashboard run.
type Options struct {
	// Interval is the delay between frames. A dashboard does not need to be
	// fast; one second is responsive without wasting USB bandwidth.
	Interval time.Duration
	// Quality is the JPEG quality, 1-100.
	Quality int
	// Theme is the colour scheme.
	Theme Theme
	// Log receives one line per frame when non-nil.
	Log io.Writer
}

// DefaultOptions returns sensible defaults for a panel dashboard.
func DefaultOptions() Options {
	return Options{
		Interval: time.Second,
		Quality:  80,
		Theme:    DefaultTheme,
	}
}

// Run draws the dashboard until ctx is cancelled.
//
// Each iteration re-reads the metrics, renders a frame, and pushes it as a
// JPEG. Errors from individual collections are tolerated (a sensor may
// disappear, or a counter may briefly be unreadable) so the dashboard keeps
// running rather than dying on a transient condition.
func Run(ctx context.Context, dev Pusher, opts Options) error {
	if opts.Interval <= 0 {
		opts.Interval = time.Second
	}
	if opts.Quality <= 0 {
		opts.Quality = 80
	}

	w, h := dev.Portrait()

	var prev metrics.Snapshot
	havePrev := false
	frames := 0

	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()

	for {
		cur, err := metrics.Collect()
		if err != nil && !havePrev {
			return fmt.Errorf("collect metrics: %w", err)
		}

		var rates metrics.Rates
		if havePrev {
			rates = metrics.Delta(prev, cur)
		}

		img := Render(cur, rates, w, h, opts.Theme)
		frame, err := encodeFrame(img, opts.Quality)
		if err != nil {
			return err
		}
		if err := dev.ShowJPEG(frame); err != nil {
			return fmt.Errorf("push frame: %w", err)
		}

		frames++
		if opts.Log != nil {
			fmt.Fprintf(opts.Log, "frame %d: cpu %.0f%%  mem %.0f%%  down %s  up %s\n",
				frames, rates.CPUPercent, cur.Mem.UsedPercent(),
				humanRate(rates.RxPerSec), humanRate(rates.TxPerSec))
		}

		prev, havePrev = cur, true

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// encodeFrame encodes a frame at the requested quality, lowering quality if
// necessary to stay inside the protocol's per-transfer payload limit.
func encodeFrame(img image.Image, quality int) ([]byte, error) {
	for q := quality; q >= 30; q -= 10 {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
			return nil, fmt.Errorf("encode frame: %w", err)
		}
		if buf.Len() <= proto.MaxPayload {
			return buf.Bytes(), nil
		}
	}
	return nil, fmt.Errorf("dashboard frame exceeds the device's %d byte payload limit", proto.MaxPayload)
}
