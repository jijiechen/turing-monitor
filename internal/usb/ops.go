package usb

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"time"

	"github.com/jijiechen/turing-monitor/internal/proto"
)

// defaultChunkSize is used when the device does not report a usable H.264
// chunk size. It matches the value the reference implementation falls back to.
const defaultChunkSize = 202752

// queueDepthLimit is the playback queue depth above which we pause feeding the
// device, so it can catch up instead of dropping frames.
const queueDepthLimit = 3

// Sync performs the protocol handshake. It is harmless to call repeatedly.
func (d *Device) Sync() error {
	block, err := proto.Command(proto.CmdSync, time.Now())
	if err != nil {
		return err
	}
	return d.SendAck(block)
}

// SetBrightness sets the backlight from a 0-100 percentage.
func (d *Device) SetBrightness(percent int) error {
	level, err := proto.BrightnessPercent(percent)
	if err != nil {
		return err
	}
	block, err := proto.Brightness(level, time.Now())
	if err != nil {
		return err
	}
	return d.SendAck(block)
}

// SetRotation sets the display rotation, 0-3.
func (d *Device) SetRotation(rotation int) error {
	if rotation < 0 || rotation > 3 {
		return fmt.Errorf("rotation %d out of range 0-3", rotation)
	}
	block, err := proto.Rotate(byte(rotation), time.Now())
	if err != nil {
		return err
	}
	return d.SendAck(block)
}

// SetFrameRate sets the playback frame rate in frames per second.
func (d *Device) SetFrameRate(fps int) error {
	if fps < 1 || fps > 255 {
		return fmt.Errorf("frame rate %d out of range 1-255", fps)
	}
	block, err := proto.FrameRate(byte(fps), time.Now())
	if err != nil {
		return err
	}
	return d.SendAck(block)
}

// Reset reboots the panel.
func (d *Device) Reset() error {
	block, err := proto.Command(proto.CmdRestart, time.Now())
	if err != nil {
		return err
	}
	return d.SendAck(block)
}

// ShowJPEG displays a JPEG image.
func (d *Device) ShowJPEG(jpeg []byte) error {
	payload, err := proto.Upload(proto.CmdUploadJPEG, jpeg, time.Now())
	if err != nil {
		return err
	}
	return d.SendAck(payload)
}

// ShowPNG displays a PNG image.
func (d *Device) ShowPNG(pngData []byte) error {
	payload, err := proto.Upload(proto.CmdUploadPNG, pngData, time.Now())
	if err != nil {
		return err
	}
	return d.SendAck(payload)
}

// Clear blanks the panel.
//
// Two deviations from the reference implementation, both forced by observed
// device behaviour:
//
//   - It clears with a black PNG hardcoded to the 8.8" panel's 480x1920
//     geometry, which is wrong for other models. We use this panel's own
//     resolution.
//   - It uses the PNG command (102). On the 5.2" panel that command is
//     acknowledged but has no visible effect, so we use the JPEG command (101)
//     instead, which is demonstrably reliable.
func (d *Device) Clear() error {
	w, h := d.model.Portrait()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	drawBlack(img)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		return fmt.Errorf("encode clear image: %w", err)
	}
	return d.ShowJPEG(buf.Bytes())
}

func drawBlack(img *image.RGBA) {
	b := img.Bounds()
	black := color.RGBA{A: 255}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			img.SetRGBA(x, y, black)
		}
	}
}

// StorageInfo reports SD card usage.
func (d *Device) StorageInfo() (proto.Storage, error) {
	block, err := proto.Command(proto.CmdStorageInfo, time.Now())
	if err != nil {
		return proto.Storage{}, err
	}
	resp, err := d.Send(block)
	if err != nil {
		return proto.Storage{}, err
	}
	return proto.ParseStorage(resp)
}

// ChunkSize negotiates the maximum H.264 chunk size the device will accept.
func (d *Device) ChunkSize() int {
	block, err := proto.Command(proto.CmdGetH264ChunkSz, time.Now())
	if err != nil {
		return defaultChunkSize
	}
	resp, err := d.Send(block)
	if err != nil {
		return defaultChunkSize
	}
	return proto.ParseChunkSize(resp, defaultChunkSize)
}

// StopStream ends H.264 playback.
func (d *Device) StopStream() error {
	block, err := proto.Command(proto.CmdStopStream, time.Now())
	if err != nil {
		return err
	}
	return d.SendAck(block)
}

// queueDepth asks the device how much playback is still queued.
func (d *Device) queueDepth() int {
	block, err := proto.Command(proto.CmdStreamStatus, time.Now())
	if err != nil {
		return 0
	}
	resp, err := d.Send(block)
	if err != nil {
		return 0
	}
	return proto.ParseQueueDepth(resp)
}

// waitForQueue blocks while the device's playback queue is backed up, so we
// feed it at a rate it can sustain rather than overrunning it.
func (d *Device) waitForQueue() {
	for i := 0; i < 20; i++ {
		if d.queueDepth() <= queueDepthLimit {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// BeginPlayback puts the panel into streaming mode.
//
// This prelude is load-bearing: without it the panel happily consumes H.264
// chunks (the playback queue drains normally) but never leaves the last
// displayed frame on screen. The sequence matches the vendor application's
// video path.
func (d *Device) BeginPlayback(fps int) error {
	// Halt anything already playing and reset the video pipeline. These are
	// sent as bare command headers, with no payload byte.
	for _, cmd := range []byte{proto.CmdStopVideo, proto.CmdResetVideo, proto.CmdRotate, proto.CmdStreamFlag} {
		block, err := proto.Command(cmd, time.Now())
		if err != nil {
			return err
		}
		if _, err := d.Send(block); err != nil {
			return fmt.Errorf("%s: %w", proto.CommandName(cmd), err)
		}
	}
	// Blank the screen so residue from the previous image does not show through
	// the stream.
	if err := d.Clear(); err != nil {
		return fmt.Errorf("clear: %w", err)
	}
	return d.SetFrameRate(fps)
}

// StreamH264 sends an Annex-B H.264 elementary stream, splitting it into chunks
// the device accepts. The final chunk is flagged so the device closes the
// playback session cleanly.
//
// The stream is read incrementally, so a large file need not be held in memory.
func (d *Device) StreamH264(r io.Reader) error {
	br := bufio.NewReaderSize(r, 64*1024)
	chunk := make([]byte, d.ChunkSize())

	for {
		n, err := io.ReadFull(br, chunk)
		if n > 0 {
			// Peeking one byte tells us whether more data follows. ReadFull
			// only returns a non-nil error when it hit the end of the stream,
			// so a failed peek always agrees with a non-nil err here.
			_, peekErr := br.Peek(1)
			final := peekErr != nil

			if err := d.sendChunk(chunk[:n], final); err != nil {
				return err
			}
			if final {
				return nil
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil // empty stream, or the tail was just sent
			}
			return fmt.Errorf("read stream: %w", err)
		}
	}
}

func (d *Device) sendChunk(data []byte, final bool) error {
	payload, err := proto.H264Chunk(data, final, time.Now())
	if err != nil {
		return err
	}
	if _, err := d.Send(payload); err != nil {
		return err
	}
	// Only apply back-pressure between chunks; the final chunk ends playback.
	if !final {
		d.waitForQueue()
	}
	return nil
}
