//go:build linux

package vdisplay

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Create finds an existing virtual X11 output and returns it.
//
// Unlike macOS, Linux gives a userspace program no way to add a display: both
// supported mechanisms need a kernel module (evdi) or a device section in
// xorg.conf (the dummy driver), plus a server restart. So this locates an
// output that has already been configured rather than creating one, and the
// error it returns when there is none explains how to set one up.
//
// This requires an X11 session. Wayland is not supported: its compositors
// offer no way to create a virtual output, and screen capture goes through a
// portal rather than the X screen.
func Create(opts Options) (*Display, error) {
	opts, err := opts.normalise()
	if err != nil {
		return nil, err
	}

	// evdi first: it creates a real DRM device with its own framebuffer, so
	// there is no screen to capture and no X server involved at all. The X11
	// path below is the fallback for systems without the kernel module.
	evdiSource, evdiErr := openEVDI(opts)
	if evdiErr == nil {
		return &Display{
			name:   opts.Name,
			width:  opts.Width,
			height: opts.Height,
			source: evdiSource,
		}, nil
	}

	if os.Getenv("DISPLAY") == "" {
		return nil, &UnsupportedError{
			Platform: "Linux without X11",
			Detail: "evdi could not be used (" + evdiErr.Error() + ") " +
				"and no DISPLAY is set, so there is no X screen to capture either",
			Hint: "either install the evdi kernel module, which also removes the need for X:\n" +
				"          sudo apt install evdi-dkms && sudo modprobe evdi\n" +
				`        or log in with an "Ubuntu on Xorg" session`,
		}
	}
	if _, err := exec.LookPath("xrandr"); err != nil {
		return nil, &UnsupportedError{
			Platform: "Linux",
			Detail: "evdi could not be used (" + evdiErr.Error() + ") " +
				"and xrandr is not installed, so outputs cannot be inspected",
			Hint: "sudo apt install x11-xserver-utils, or install evdi",
		}
	}

	out, err := exec.Command("xrandr", "--query").Output()
	if err != nil {
		return nil, fmt.Errorf("run xrandr: %w", err)
	}
	outputs := parseXrandr(out)
	if len(outputs) == 0 {
		return nil, &UnsupportedError{
			Platform: "Linux",
			Detail:   "xrandr reported no connected outputs",
		}
	}

	if opts.Output != "" {
		for _, o := range outputs {
			if o.name == opts.Output {
				return displayFrom(o, opts, evdiErr), nil
			}
		}
		return nil, &UnsupportedError{
			Platform: "Linux",
			Detail:   fmt.Sprintf("no output named %q; available: %s", opts.Output, outputNames(outputs)),
		}
	}

	if o, ok := pickVirtualOutput(outputs, opts.Width, opts.Height); ok {
		return displayFrom(o, opts, evdiErr), nil
	}

	return nil, &UnsupportedError{
		Platform: "Linux",
		Detail:   "no virtual output found among the connected outputs: " + outputNames(outputs),
		Hint: "create one first, either:\n" +
			"          sudo apt install evdi-dkms && sudo modprobe evdi\n" +
			"        or add a dummy device to xorg.conf:\n" +
			"          Section \"Device\"\n" +
			"            Identifier \"Virtual\"\n" +
			"            Driver \"dummy\"\n" +
			"            VideoRam 32768\n" +
			"            Option \"ConnectedMonitor\" \"DP-1\"\n" +
			"          EndSection\n" +
			"        then select it with --output <name>",
	}
}

// displayFrom converts a probed output into a Display. evdiErr is carried
// along so the caller can report that this display came from the fallback.
func displayFrom(o output, opts Options, evdiErr error) *Display {
	w, h := opts.Width, opts.Height
	if w <= 0 || h <= 0 {
		w, h = o.width, o.height
	}
	// On Linux capture is addressed by screen region, so the origin matters.
	// sourceErr carries the reason evdi was not used, so the caller can say
	// which path it fell back to.
	return &Display{name: o.name, x: o.x, y: o.y, width: w, height: h, sourceErr: evdiErr}
}

func outputNames(outputs []output) string {
	names := make([]string, 0, len(outputs))
	for _, o := range outputs {
		names = append(names, o.name)
	}
	return strings.Join(names, ", ")
}

// Close is a no-op on Linux: the output belongs to the system configuration,
// not to this process, so nothing is torn down.
func (d *Display) Close() error { return nil }

// SettleDelay is how long to wait after choosing the display before capturing
// it. The output already exists on Linux, so there is nothing to wait for.
const SettleDelay = 0
