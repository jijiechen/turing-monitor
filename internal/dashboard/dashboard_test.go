package dashboard

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"testing"
	"time"

	"github.com/jijiechen/turing-monitor/internal/metrics"
	"github.com/jijiechen/turing-monitor/internal/proto"
)

// sample returns a populated snapshot covering every section of the layout.
func sample() metrics.Snapshot {
	return metrics.Snapshot{
		Time:     time.Unix(1000, 0),
		Hostname: "test-host",
		Uptime:   72*time.Hour + 30*time.Minute,
		Load:     [3]float64{1.5, 2.0, 2.5},
		CPU:      metrics.CPUTimes{User: 100, System: 50, Idle: 850},
		Mem:      metrics.MemInfo{Total: 32 << 30, Used: 24 << 30, Available: 8 << 30},
		Swap:     metrics.MemInfo{Total: 4 << 30, Used: 1 << 30, Available: 3 << 30},
		Net:      metrics.NetCounters{RxBytes: 1000, TxBytes: 2000},
		Temps:    []metrics.Sensor{{Name: "cpu", Celsius: 42.5}, {Name: "nvme", Celsius: 61.0}},
		Mounts:   []metrics.Mount{{Path: "/", Total: 1000 << 30, Used: 600 << 30}},
	}
}

func TestRenderSizeAndBounds(t *testing.T) {
	const w, h = 720, 1280
	img := Render(sample(), metrics.Rates{CPUPercent: 15, RxPerSec: 4096, TxPerSec: 2048}, w, h, DefaultTheme)

	if got := img.Bounds().Dx(); got != w {
		t.Errorf("width = %d, want %d", got, w)
	}
	if got := img.Bounds().Dy(); got != h {
		t.Errorf("height = %d, want %d", got, h)
	}
	// The background is dark; the CPU accent should have painted something.
	if bytes.Equal(img.Pix, make([]byte, len(img.Pix))) {
		t.Error("frame is entirely zero-valued; nothing was drawn")
	}
}

// TestRenderDegenerateInputs covers the cases a real machine can produce: a
// brand new boot with no counters, and a platform with no temperature sensors
// (macOS).
func TestRenderDegenerateInputs(t *testing.T) {
	cases := map[string]metrics.Snapshot{
		"zero value":   {},
		"no temps":     {Hostname: "x", Mem: metrics.MemInfo{Total: 1}, Mounts: []metrics.Mount{{Path: "/", Total: 100, Used: 50}}},
		"no mounts":    {Hostname: "x", Mem: metrics.MemInfo{Total: 1}, Temps: []metrics.Sensor{{Name: "t", Celsius: 40}}},
		"swap absent":  {Hostname: "x"},
		"many sensors": {Hostname: "x", Temps: make([]metrics.Sensor, 20)},
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			// A panic here is the failure mode we care about.
			img := Render(s, metrics.Rates{}, 720, 1280, DefaultTheme)
			if img == nil {
				t.Fatal("Render returned nil")
			}
		})
	}
}

func TestSeverityThresholds(t *testing.T) {
	th := DefaultTheme
	if got := th.severity(10); got != th.Accent {
		t.Error("low usage should use the accent colour")
	}
	if got := th.severity(80); got != th.Warn {
		t.Error("high usage should use the warning colour")
	}
	if got := th.severity(95); got != th.Crit {
		t.Error("critical usage should use the critical colour")
	}
}

func TestHumanHelpers(t *testing.T) {
	if got := humanBytes(0); got != "0 B" {
		t.Errorf("humanBytes(0) = %q", got)
	}
	if got := humanBytes(1536); got != "1.5 KB" {
		t.Errorf("humanBytes(1536) = %q, want 1.5 KB", got)
	}
	if got := humanRate(500); got != "500 B/s" {
		t.Errorf("humanRate(500) = %q", got)
	}
	if got := humanRate(2048); got != "2 KB/s" {
		t.Errorf("humanRate(2048) = %q", got)
	}
	if got := humanRate(2_500_000); got != "2.5 MB/s" {
		t.Errorf("humanRate(2.5e6) = %q", got)
	}
	if got := humanDuration(72*time.Hour + 30*time.Minute); got != "3d 0h" {
		t.Errorf("humanDuration = %q, want 3d 0h", got)
	}
	if got := humanDuration(90 * time.Minute); got != "1h 30m" {
		t.Errorf("humanDuration = %q, want 1h 30m", got)
	}
}

// fakeDevice records the frames pushed to it.
type fakeDevice struct {
	frames [][]byte
	w, h   int
}

func (f *fakeDevice) ShowJPEG(b []byte) error {
	f.frames = append(f.frames, b)
	return nil
}
func (f *fakeDevice) Portrait() (int, int) { return f.w, f.h }

func TestRunPushesFramesUntilCancelled(t *testing.T) {
	dev := &fakeDevice{w: 720, h: 1280}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, dev, Options{Interval: 20 * time.Millisecond, Quality: 60, Theme: DefaultTheme})
	}()

	// Let a few frames through, then stop.
	deadline := time.After(2 * time.Second)
	for len(dev.frames) < 3 {
		select {
		case <-deadline:
			cancel()
			t.Fatalf("only %d frames pushed, want at least 3", len(dev.frames))
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}

	// Every pushed frame must be a decodable JPEG of the panel's size.
	img, err := jpeg.Decode(bytes.NewReader(dev.frames[0]))
	if err != nil {
		t.Fatalf("pushed frame is not a valid JPEG: %v", err)
	}
	if img.Bounds() != image.Rect(0, 0, 720, 1280) {
		t.Errorf("frame bounds = %v, want 720x1280", img.Bounds())
	}
}

func TestEncodeFrameRespectsPayloadLimit(t *testing.T) {
	// A noisy image is expensive to encode, forcing the quality to drop.
	img := image.NewRGBA(image.Rect(0, 0, 720, 1280))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 7)
	}
	data, err := encodeFrame(img, 95)
	if err != nil {
		t.Fatalf("encodeFrame: %v", err)
	}
	if len(data) > proto.MaxPayload {
		t.Errorf("frame is %d bytes, over the %d limit", len(data), proto.MaxPayload)
	}
}
