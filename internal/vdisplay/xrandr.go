package vdisplay

import (
	"bufio"
	"regexp"
	"strconv"
	"strings"
)

// This file holds the xrandr output parsing, separated from the Linux-only
// file that runs the command.
//
// The parsing is where the mistakes would be, and keeping it free of build
// tags means it can be tested on any platform rather than only on a Linux
// machine with an X server.

// output is one connected X11 output.
type output struct {
	name           string
	width, height  int
	x, y           int
	physicalWidth  int
	physicalHeight int
}

// outputLine matches an xrandr output entry, e.g.
//
//	DUMMY0 connected 720x1280+0+0 (normal left inverted right x axis y axis) 0mm x 0mm
//	HDMI-1 connected primary 1920x1080+720+0 (normal ...) 509mm x 286mm
//
// Only "connected" outputs have a geometry; disconnected ones appear as
// "HDMI-1 disconnected (normal ...)" and are skipped because they cannot be
// captured.
var outputLine = regexp.MustCompile(
	`^(\S+) connected(?:\s+primary)?\s+(\d+)x(\d+)\+(-?\d+)\+(-?\d+).*?(\d+)mm x (\d+)mm`)

// parseXrandr extracts connected outputs from `xrandr --query` output.
func parseXrandr(data []byte) []output {
	var outputs []output
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		m := outputLine.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		outputs = append(outputs, output{
			name:           m[1],
			width:          atoi(m[2]),
			height:         atoi(m[3]),
			x:              atoi(m[4]),
			y:              atoi(m[5]),
			physicalWidth:  atoi(m[6]),
			physicalHeight: atoi(m[7]),
		})
	}
	return outputs
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// pickVirtualOutput chooses the output most likely to be a virtual display.
//
// Two signals, in order of reliability:
//
//   - An exact match on the requested geometry. An output already sized to the
//     panel is almost certainly the one configured for this purpose.
//   - A physical size of 0mm, which is what a driver reports when no real
//     monitor is attached. The dummy driver and evdi both do this, whereas a
//     real panel always reports its dimensions.
func pickVirtualOutput(outputs []output, width, height int) (output, bool) {
	if width > 0 && height > 0 {
		for _, o := range outputs {
			if o.width == width && o.height == height {
				return o, true
			}
		}
	}
	for _, o := range outputs {
		if o.physicalWidth == 0 || o.physicalHeight == 0 {
			return o, true
		}
	}
	return output{}, false
}
