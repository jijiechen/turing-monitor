// Command turzx drives TURZX / 图灵智显 USB panels (the 0x1cbe family) from
// macOS and Linux.
//
// The panels speak a vendor protocol over two bulk endpoints, so no kernel
// driver is required on either platform. See docs/PROTOCOL.md for the wire
// format, which was recovered by reverse-engineering the vendor's Windows
// application.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jijiechen/turing-monitor/internal/dashboard"
	"github.com/jijiechen/turing-monitor/internal/media"
	"github.com/jijiechen/turing-monitor/internal/usb"
)

const usage = `turzx — drive a TURZX / 图灵智显 USB display

Usage:
  turzx info                     list attached panels
  turzx sync                     handshake with the panel
  turzx clear                    blank the screen
  turzx image <file>             display a JPEG or PNG, scaled to fit
  turzx video [--loop] [--fps N] <file>
                                 stream an MP4 or raw Annex-B H.264 file
  turzx brightness <0-100>       set the backlight
  turzx rotate <0-3>             set the display rotation
  turzx dashboard [--interval 1s]
                                 live system monitor on the panel
  turzx storage                  report the panel's SD card usage
  turzx extract <in.mp4> <out.h264>
                                 convert an MP4 to raw Annex-B H.264

Add --verbose to log each frame while the dashboard runs.\nRun with no arguments for this message.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "turzx: %v\n", err)
		os.Exit(1)
	}
}

// verbose enables per-frame logging for long-running commands.
var verbose bool

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}

	// --verbose may appear anywhere on the command line, so strip it before
	// dispatching.
	filtered := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--verbose" || a == "-v" {
			verbose = true
			continue
		}
		filtered = append(filtered, a)
	}
	args = filtered
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}

	cmd, rest := args[0], args[1:]

	// `info` deliberately does not claim the panel, so it works even while a
	// stream is running.
	if cmd == "info" {
		return cmdInfo()
	}
	if cmd == "help" || cmd == "-h" || cmd == "--help" {
		fmt.Print(usage)
		return nil
	}

	dev, err := usb.Open()
	if err != nil {
		return err
	}
	defer dev.Close()

	switch cmd {
	case "sync":
		return dev.Sync()

	case "clear":
		return dev.Clear()

	case "image":
		if len(rest) != 1 {
			return errors.New("usage: turzx image <file>")
		}
		return cmdImage(dev, rest[0])

	case "video":
		opts, err := parseVideoArgs(rest)
		if err != nil {
			return err
		}
		return cmdVideo(dev, opts)

	case "brightness":
		if len(rest) != 1 {
			return errors.New("usage: turzx brightness <0-100>")
		}
		percent, err := strconv.Atoi(rest[0])
		if err != nil {
			return fmt.Errorf("invalid brightness %q: expected a number 0-100", rest[0])
		}
		return dev.SetBrightness(percent)

	case "rotate":
		if len(rest) != 1 {
			return errors.New("usage: turzx rotate <0-3>")
		}
		rotation, err := strconv.Atoi(rest[0])
		if err != nil {
			return fmt.Errorf("invalid rotation %q: expected a number 0-3", rest[0])
		}
		return dev.SetRotation(rotation)

	case "extract":
		if len(rest) != 2 {
			return errors.New("usage: turzx extract <input.mp4> <output.h264>")
		}
		return cmdExtract(rest[0], rest[1])

	case "dashboard":
		interval, err := parseInterval(rest)
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		opts := dashboard.DefaultOptions()
		opts.Interval = interval
		if verbose {
			opts.Log = os.Stdout
		}
		fmt.Printf("dashboard on %s, refreshing every %s — press Ctrl-C to stop\n", dev.Model(), interval)
		return dashboard.Run(ctx, dev, opts)

	case "storage":
		s, err := dev.StorageInfo()
		if err != nil {
			return err
		}
		fmt.Printf("total %s\nused  %s\nvalid %s\n", humanBytes(s.Total), humanBytes(s.Used), humanBytes(s.Valid))
		return nil

	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func cmdInfo() error {
	infos, err := usb.Discover()
	if err != nil {
		return err
	}
	if len(infos) == 0 {
		return usb.ErrNotFound
	}
	for _, info := range infos {
		fmt.Println(info)
	}
	return nil
}

func cmdImage(dev *usb.Device, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	jpeg, err := prepareImage(data, dev.Model())
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	fmt.Printf("sending %s as %d bytes of JPEG (%s)\n", path, len(jpeg), dev.Model())
	return dev.ShowJPEG(jpeg)
}

// videoOpts holds the parsed flags for the video subcommand.
type videoOpts struct {
	path   string
	loop   bool
	fps    int // 0 means "derive from the file, or fall back to defaultFPS"
	fpsSet bool
}

// defaultFPS is used for raw H.264 files, which carry no timing information.
const defaultFPS = 25

// parseVideoArgs accepts: [--fps N] [--loop] <file>
func parseVideoArgs(args []string) (videoOpts, error) {
	var opts videoOpts
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--loop" || arg == "-loop":
			opts.loop = true
		case arg == "--fps":
			if i+1 >= len(args) {
				return opts, errors.New("--fps needs a value")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil {
				return opts, fmt.Errorf("invalid --fps %q: expected a number", args[i])
			}
			opts.fps, opts.fpsSet = n, true
		case strings.HasPrefix(arg, "-"):
			return opts, fmt.Errorf("unknown flag %q", arg)
		default:
			if opts.path != "" {
				return opts, errors.New("usage: turzx video [--fps N] [--loop] <file>")
			}
			opts.path = arg
		}
	}
	if opts.path == "" {
		return opts, errors.New("usage: turzx video [--fps N] [--loop] <file>")
	}
	return opts, nil
}

func cmdVideo(dev *usb.Device, opts videoOpts) error {
	f, err := os.Open(opts.path)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	isMP4 := false
	switch ext := strings.ToLower(filepath.Ext(opts.path)); ext {
	case ".mp4", ".m4v", ".mov":
		isMP4 = true
	}

	// The panel plays frames as they arrive, so it needs to be told the
	// intended rate or playback runs as fast as the USB link delivers.
	fps := opts.fps
	if fps == 0 {
		fps = defaultFPS
		if isMP4 {
			if probed, err := media.ProbeFPS(f, info.Size()); err == nil && probed >= 1 {
				fps = int(math.Round(probed))
				fmt.Printf("source is %.2f fps\n", probed)
			}
		}
	}
	if fps < 1 {
		fps = 1
	}
	if fps > 255 {
		fps = 255
	}
	fmt.Printf("playing at %d fps\n", fps)
	if err := dev.BeginPlayback(fps); err != nil {
		return err
	}

	for {
		if err := streamOnce(dev, f, info.Size(), isMP4, opts.path); err != nil {
			return err
		}
		if !opts.loop {
			break
		}
	}
	return dev.StopStream()
}

// streamOnce sends one pass over the file. For MP4 the file is demuxed to an
// Annex-B elementary stream on the fly, so nothing is buffered to disk.
func streamOnce(dev *usb.Device, f *os.File, size int64, isMP4 bool, path string) error {
	src := io.Reader(f)
	if isMP4 {
		pr, pw := io.Pipe()
		defer pr.Close()
		go func() {
			pw.CloseWithError(media.ExtractAnnexB(f, size, pw))
		}()
		src = pr
	}
	fmt.Printf("streaming %s to %s\n", filepath.Base(path), dev.Model())
	return dev.StreamH264(src)
}

// parseInterval accepts: [--interval <duration>]
func parseInterval(args []string) (time.Duration, error) {
	interval := time.Second
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--interval":
			if i+1 >= len(args) {
				return 0, errors.New("--interval needs a value, e.g. 500ms or 2s")
			}
			i++
			d, err := time.ParseDuration(args[i])
			if err != nil {
				return 0, fmt.Errorf("invalid --interval %q: %w", args[i], err)
			}
			if d < 100*time.Millisecond {
				return 0, errors.New("--interval must be at least 100ms")
			}
			interval = d
		case strings.HasPrefix(arg, "-"):
			return 0, fmt.Errorf("unknown flag %q", arg)
		default:
			return 0, fmt.Errorf("unexpected argument %q", arg)
		}
	}
	return interval, nil
}

// cmdExtract converts an MP4 to a raw Annex-B H.264 file, so the stream can be
// inspected or fed to other tools. It does not touch the panel.
func cmdExtract(in, out string) error {
	f, err := os.Open(in)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}

	w, err := os.Create(out)
	if err != nil {
		return err
	}
	defer w.Close()

	if err := media.ExtractAnnexB(f, info.Size(), w); err != nil {
		return err
	}
	st, err := w.Stat()
	if err != nil {
		return err
	}
	fmt.Printf("wrote %s (%s)\n", out, humanBytes(st.Size()))
	return nil
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(n)/float64(div), "KMGT"[exp])
}
