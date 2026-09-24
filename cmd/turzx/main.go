// Command turzx drives TURZX / 图灵智显 USB panels (the 0x1cbe family) from
// macOS and Linux.
//
// The panels speak a vendor protocol over two bulk endpoints, so no kernel
// driver is required on either platform. See docs/PROTOCOL.md for the wire
// format.
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

	"github.com/jijiechen/turing-monitor/internal/capture"
	"github.com/jijiechen/turing-monitor/internal/dashboard"
	"github.com/jijiechen/turing-monitor/internal/media"
	"github.com/jijiechen/turing-monitor/internal/orient"
	"github.com/jijiechen/turing-monitor/internal/usb"
	"github.com/jijiechen/turing-monitor/internal/vdisplay"
)

const usage = `turzx — drive a TURZX / 图灵智显 USB display

Usage:
  turzx info                     list attached panels
  turzx sync                     handshake with the panel
  turzx clear                    blank the screen
  turzx image [--orientation NAME] <file>
                                 display a JPEG or PNG, scaled to fit
                                 NAME describes how the panel is mounted:
                                   portrait            panel as it comes (default)
                                   landscape           turned 90° anticlockwise, right edge up
                                   portrait-inverted   panel upside down
                                   landscape-inverted  turned 90° clockwise, left edge up
  turzx video [--loop] [--fps N] <file>
                                 stream an MP4 or raw Annex-B H.264 file
  turzx brightness <0-100>       set the backlight
  turzx rotate <0-3>             set the display rotation
  turzx monitor [--fps 30] [--bitrate 16000] [--orientation NAME]
                [--output NAME] [--size WxH]
                                 use the panel as an extended desktop
  turzx dashboard [--interval 1s] [--orientation NAME]
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

	// These commands look after the panel themselves: they have to survive it
	// being unplugged and plugged back in, so they open and reopen the device
	// rather than borrowing one opened here.
	switch cmd {
	case "monitor":
		opts, err := parseMonitorArgs(rest)
		if err != nil {
			return err
		}
		return cmdMonitor(context.Background(), opts)
	case "dashboard":
		opts, err := parseDashboardArgs(rest)
		if err != nil {
			return err
		}
		return cmdDashboard(context.Background(), opts)
	}

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
		imgOpts, err := parseImageArgs(rest)
		if err != nil {
			return err
		}
		return cmdImage(dev, imgOpts)

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

// imageOpts holds the parsed flags for the image subcommand.
type imageOpts struct {
	path        string
	orientation orient.Orientation
}

// parseImageArgs accepts: [--orientation NAME] <file>
func parseImageArgs(args []string) (imageOpts, error) {
	opts := imageOpts{orientation: orient.Portrait}
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--orientation" || arg == "--rotate":
			if i+1 >= len(args) {
				return opts, errors.New("--orientation needs a value: " + orientationNames())
			}
			i++
			o, err := orient.Parse(args[i])
			if err != nil {
				return opts, err
			}
			opts.orientation = o
		case strings.HasPrefix(arg, "-"):
			return opts, fmt.Errorf("unknown flag %q", arg)
		default:
			if opts.path != "" {
				return opts, errors.New("usage: turzx image [--orientation NAME] <file>")
			}
			opts.path = arg
		}
	}
	if opts.path == "" {
		return opts, errors.New("usage: turzx image [--orientation NAME] <file>")
	}
	return opts, nil
}

// orientationNames lists the accepted values, for error messages and help.
func orientationNames() string {
	names := make([]string, 0, 4)
	for _, o := range orient.All() {
		names = append(names, o.String())
	}
	return strings.Join(names, ", ")
}

func cmdImage(dev *usb.Device, opts imageOpts) error {
	data, err := os.ReadFile(opts.path)
	if err != nil {
		return err
	}
	jpeg, err := prepareImage(data, dev.Model(), opts.orientation)
	if err != nil {
		return fmt.Errorf("%s: %w", opts.path, err)
	}
	cw, ch := opts.orientation.ContentSize(dev.Portrait())
	fmt.Printf("sending %s as %d bytes of JPEG (%s, %s %dx%d)\n",
		opts.path, len(jpeg), dev.Model(), opts.orientation, cw, ch)
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

// monitorOpts holds the parsed flags for the monitor subcommand.
type monitorOpts struct {
	fps     int
	bitrate int
	// output names a specific display output, for platforms where the virtual
	// display has to be configured ahead of time rather than created here.
	output string
	// width and height size the virtual display when no panel is attached to
	// ask. A service may well start before the panel is plugged in.
	width, height int
	// orientation is how the panel is physically mounted. The virtual display
	// is created at the size the viewer sees, and frames are rotated on the
	// way to the encoder.
	orientation orient.Orientation
}

// parseMonitorArgs accepts: [--fps N] [--bitrate KBPS]
func parseMonitorArgs(args []string) (monitorOpts, error) {
	opts := monitorOpts{fps: 30, bitrate: 16_000_000, width: 720, height: 1280, orientation: orient.Portrait}
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--fps":
			if i+1 >= len(args) {
				return opts, errors.New("--fps needs a value")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 1 || n > 60 {
				return opts, fmt.Errorf("invalid --fps %q: expected 1-60", args[i])
			}
			opts.fps = n
		case arg == "--bitrate":
			if i+1 >= len(args) {
				return opts, errors.New("--bitrate needs a value in kbit/s")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 100 {
				return opts, fmt.Errorf("invalid --bitrate %q: expected kbit/s, at least 100", args[i])
			}
			opts.bitrate = n * 1000
		case arg == "--orientation" || arg == "--rotate":
			if i+1 >= len(args) {
				return opts, errors.New("--orientation needs a value: " + orientationNames())
			}
			i++
			o, err := orient.Parse(args[i])
			if err != nil {
				return opts, err
			}
			opts.orientation = o
		case arg == "--size":
			if i+1 >= len(args) {
				return opts, errors.New("--size needs a value like 720x1280")
			}
			i++
			w, h, err := parseSize(args[i])
			if err != nil {
				return opts, err
			}
			opts.width, opts.height = w, h
		case arg == "--output":
			if i+1 >= len(args) {
				return opts, errors.New("--output needs a value, e.g. DUMMY0")
			}
			i++
			opts.output = args[i]
		case strings.HasPrefix(arg, "-"):
			return opts, fmt.Errorf("unknown flag %q", arg)
		default:
			return opts, fmt.Errorf("unexpected argument %q", arg)
		}
	}
	return opts, nil
}

// parseSize reads a WxH dimension pair.
func parseSize(s string) (int, int, error) {
	ws, hs, ok := strings.Cut(s, "x")
	if !ok {
		return 0, 0, fmt.Errorf("invalid --size %q: expected WxH, e.g. 720x1280", s)
	}
	w, err1 := strconv.Atoi(strings.TrimSpace(ws))
	h, err2 := strconv.Atoi(strings.TrimSpace(hs))
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("invalid --size %q: expected WxH, e.g. 720x1280", s)
	}
	return w, h, nil
}

// displayLabel names a virtual display for logging, using whichever identifier
// the platform actually provides.
func displayLabel(d *vdisplay.Display) string {
	if name := d.Name(); name != "" {
		return name
	}
	return fmt.Sprintf("%d", d.ID())
}

// cmdMonitor makes the panel a real extended desktop: it creates a virtual
// display, captures it, and streams the encoded result to the panel.
//
// The display and the capture are established once and kept for the life of
// the process. Only the USB link is torn down and rebuilt when the panel is
// unplugged and plugged back in: on macOS a recreated virtual display loses
// its position in the arrangement and takes the windows that were on it with
// it, so a display that blinks out every time someone bumps the cable would be
// worse than useless.
//
// Frames produced while no panel is attached are simply discarded.
func cmdMonitor(ctx context.Context, opts monitorOpts) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	nativeW, nativeH := opts.width, opts.height

	// If a panel is attached, prefer the resolution it reports over the
	// configured default. Discover does not claim the interface, so this is
	// safe to do before the supervised session takes the device.
	if found, err := usb.Discover(); err == nil && len(found) > 0 {
		nativeW, nativeH = found[0].Model.Portrait()
	}

	// The virtual display is the size the viewer sees, which is the panel's
	// native size only when the panel is mounted the way it comes.
	w, h := opts.orientation.ContentSize(nativeW, nativeH)

	display, err := vdisplay.Create(vdisplay.Options{
		Name:      "TURZX",
		Output:    opts.output,
		Width:     w,
		Height:    h,
		RefreshHz: 60,
	})
	if err != nil {
		return err
	}
	defer display.Close()
	fmt.Printf("virtual display %s at %dx%d (%s)\n", displayLabel(display), w, h, opts.orientation)

	// On macOS the WindowServer publishes a new display asynchronously, and
	// asking ScreenCaptureKit to enumerate it too early finds nothing. On
	// Linux the output already exists and there is nothing to wait for.
	time.Sleep(vdisplay.SettleDelay)

	originX, originY := display.Origin()
	session, err := capture.Start(capture.Options{
		DisplayID:    display.ID(),
		X:            originX,
		Y:            originY,
		Width:        w,
		Height:       h,
		QuarterTurns: int(opts.orientation),
		FPS:          opts.fps,
		Bitrate:      opts.bitrate,
	})
	if err != nil {
		return err
	}
	defer session.Close()
	fmt.Printf("capturing %dx%d, encoding %dx%d at %d fps, %d kbit/s\n",
		w, h, nativeW, nativeH, opts.fps, opts.bitrate/1000)

	return usb.Supervise(ctx, newTimestampWriter(os.Stdout), func(dev *usb.Device) error {
		return streamToPanel(ctx, dev, session, opts)
	})
}

// streamToPanel runs one streaming session: it primes the panel and feeds it
// frames until the panel goes away or the context is cancelled.
//
// The panel resets when it is unplugged, so the prelude has to be sent again on
// every reconnect; that is why it lives here rather than alongside the capture
// setup.
func streamToPanel(ctx context.Context, dev *usb.Device, session *capture.Session, opts monitorOpts) error {
	if err := dev.BeginPlayback(opts.fps); err != nil {
		return err
	}
	fmt.Printf("streaming to %s\n", dev.Model())

	frames, bytes := 0, 0
	start := time.Now()
	lastReport := start

	for {
		if ctx.Err() != nil {
			// End the stream properly so the panel is not left mid-session.
			_ = dev.StopStream()
			fmt.Printf("stopped after %d frames, %s\n", frames, humanBytes(int64(bytes)))
			return nil
		}

		frame, err := session.Next()
		if err != nil {
			return err
		}
		if err := dev.SendVideoFrame(frame); err != nil {
			return err
		}
		frames++
		bytes += len(frame)

		// Once a second, report enough to tell "the encoder is starving for
		// bits" apart from "the panel cannot keep up": if the rate sits at the
		// configured bitrate the link is saturated, and if it sits well below
		// it the source is simply static.
		if verbose && time.Since(lastReport) >= time.Second {
			elapsed := time.Since(start).Seconds()
			fmt.Printf("frames=%6d  fps=%5.1f  avg=%7s  rate=%6.0f KB/s  queue=%d\n",
				frames, float64(frames)/elapsed,
				humanBytes(int64(bytes)/int64(max(frames, 1))),
				float64(bytes)/1024/elapsed, dev.QueueDepth())
			lastReport = time.Now()
		}
	}
}

// cmdDashboard runs the system monitor on the panel, surviving the panel being
// unplugged and plugged back in.
func cmdDashboard(ctx context.Context, opts dashboardOpts) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	run := dashboard.DefaultOptions()
	run.Interval = opts.interval
	run.Log = opts.log
	run.Orientation = opts.orientation

	fmt.Printf("dashboard refreshing every %s (%s) — press Ctrl-C to stop\n",
		opts.interval, opts.orientation)

	return usb.Supervise(ctx, newTimestampWriter(os.Stdout), func(dev *usb.Device) error {
		return dashboard.Run(ctx, dev, run)
	})
}

// dashboardOpts holds the parsed flags for the dashboard subcommand.
type dashboardOpts struct {
	interval    time.Duration
	log         io.Writer
	orientation orient.Orientation
}

// parseDashboardArgs accepts: [--interval <duration>]
func parseDashboardArgs(args []string) (dashboardOpts, error) {
	opts := dashboardOpts{interval: time.Second, orientation: orient.Portrait}
	if verbose {
		opts.log = os.Stdout
	}
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--orientation" || arg == "--rotate":
			if i+1 >= len(args) {
				return opts, errors.New("--orientation needs a value: " + orientationNames())
			}
			i++
			o, err := orient.Parse(args[i])
			if err != nil {
				return opts, err
			}
			opts.orientation = o
		case arg == "--interval":
			if i+1 >= len(args) {
				return opts, errors.New("--interval needs a value, e.g. 500ms or 2s")
			}
			i++
			d, err := time.ParseDuration(args[i])
			if err != nil {
				return opts, fmt.Errorf("invalid --interval %q: %w", args[i], err)
			}
			if d < 100*time.Millisecond {
				return opts, errors.New("--interval must be at least 100ms")
			}
			opts.interval = d
		case strings.HasPrefix(arg, "-"):
			return opts, fmt.Errorf("unknown flag %q", arg)
		default:
			return opts, fmt.Errorf("unexpected argument %q", arg)
		}
	}
	return opts, nil
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
