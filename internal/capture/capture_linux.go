//go:build linux

package capture

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
)

// chunkSize is how much of the encoded stream to read per call.
//
// It trades latency against USB overhead: ffmpeg will not hand over data until
// this much has accumulated, so a large value adds delay, while a small one
// multiplies the number of USB transfers. At the default bitrate this is about
// half a frame.
const chunkSize = 32 * 1024

// Session is a running capture.
type Session struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	reader *bufio.Reader
	buf    []byte

	// stderr collects ffmpeg's diagnostics so a failure can be reported with
	// its actual reason rather than just "exit status 1".
	stderr   *bytes.Buffer
	stderrWG sync.WaitGroup

	closeOnce sync.Once
	closeErr  error
}

// Start begins capturing a screen region with ffmpeg.
//
// ffmpeg is used rather than a Go X11 binding because it handles both capture
// and H.264 encoding, including the hardware encoders, in one well-tested
// place. The output is an Annex-B elementary stream on stdout, the same shape
// the macOS backend produces.
func Start(opts Options) (*Session, error) {
	opts, err := opts.normalise()
	if err != nil {
		return nil, err
	}

	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, errors.New("capture: ffmpeg is required for screen capture on Linux; " +
			"install it with: sudo apt install ffmpeg")
	}
	display := os.Getenv("DISPLAY")
	if display == "" {
		return nil, errors.New("capture: DISPLAY is not set; " +
			"screen capture needs an X11 session (Wayland is not supported)")
	}

	// x11grab addresses the screen by region rather than by output.
	input := fmt.Sprintf("%s+%d,%d", display, opts.X, opts.Y)
	args := []string{
		"-loglevel", "error",
		"-f", "x11grab",
		"-framerate", strconv.Itoa(opts.FPS),
		"-video_size", fmt.Sprintf("%dx%d", opts.Width, opts.Height),
		"-i", input,
		"-c:v", "libx264",
		// ultrafast + zerolatency keeps the encoder from buffering, which
		// would otherwise add whole frames of delay.
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-profile:v", "high",
		"-pix_fmt", "yuv420p",
		"-b:v", strconv.Itoa(opts.Bitrate),
		// Allow bursts above the average, so frames dense with text are not
		// starved and left looking soft.
		"-maxrate", strconv.Itoa(opts.Bitrate * 3 / 2),
		"-bufsize", strconv.Itoa(opts.Bitrate * 2),
		// Refresh with an IDR once a second. A longer interval lets P-frame
		// error accumulate into trailing behind moving content.
		"-g", strconv.Itoa(opts.FPS),
		"-f", "h264",
		"-",
	}

	cmd := exec.Command(ffmpeg, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("capture: ffmpeg stdout: %w", err)
	}

	s := &Session{
		cmd:    cmd,
		stdout: stdout,
		reader: bufio.NewReaderSize(stdout, 2*chunkSize),
		buf:    make([]byte, chunkSize),
		stderr: &bytes.Buffer{},
	}
	cmd.Stderr = s.stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("capture: start ffmpeg: %w", err)
	}
	return s, nil
}

// Next returns the next slice of the encoded stream, blocking until enough
// data has accumulated.
//
// The slice is only valid until the following call.
func (s *Session) Next() ([]byte, error) {
	n, err := io.ReadFull(s.reader, s.buf)
	if n > 0 {
		return s.buf[:n], nil
	}
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("capture: ffmpeg stopped%s", s.stderrDetail())
	}
	return nil, fmt.Errorf("capture: read from ffmpeg: %w%s", err, s.stderrDetail())
}

// stderrDetail appends ffmpeg's own message, which is the useful part of any
// failure here.
func (s *Session) stderrDetail() string {
	msg := bytes.TrimSpace(s.stderr.Bytes())
	if len(msg) == 0 {
		return ""
	}
	return ": " + string(msg)
}

// Close stops ffmpeg and releases the pipe.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		if s.cmd.Process != nil {
			// Closing stdout first unblocks any pending read, so ffmpeg does
			// not have to be killed hard.
			_ = s.stdout.Close()
			if err := s.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				s.closeErr = err
			}
		}
		_ = s.cmd.Wait()
	})
	return s.closeErr
}
