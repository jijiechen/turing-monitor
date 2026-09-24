package vdisplay

import "testing"

// A condensed but realistic `xrandr --query` dump: a laptop panel, an external
// monitor, and a dummy virtual output.
const sampleXrandr = `Screen 0: minimum 320 x 200, current 2640 x 1280, maximum 16384 x 16384
eDP-1 connected primary 1920x1080+0+0 (normal left inverted right x axis y axis) 344mm x 194mm
   1920x1080     60.00*+  59.97
   1680x1050     59.95
HDMI-1 disconnected (normal left inverted right x axis y axis)
DUMMY0 connected 720x1280+1920+0 (normal left inverted right x axis y axis) 0mm x 0mm
   720x1280      60.00*+
DP-1-1 connected 480x1920+1920+0 (normal left inverted right x axis y axis) 0mm x 0mm
   480x1920      60.00*+
`

func TestParseXrandr(t *testing.T) {
	outputs := parseXrandr([]byte(sampleXrandr))

	// The disconnected output has no geometry and must be skipped.
	if len(outputs) != 3 {
		t.Fatalf("parsed %d outputs, want 3: %+v", len(outputs), outputs)
	}

	primary := outputs[0]
	if primary.name != "eDP-1" || primary.width != 1920 || primary.height != 1080 {
		t.Errorf("primary output = %+v, want eDP-1 1920x1080", primary)
	}
	if primary.x != 0 || primary.y != 0 {
		t.Errorf("primary origin = %d,%d, want 0,0", primary.x, primary.y)
	}
	if primary.physicalWidth != 344 || primary.physicalHeight != 194 {
		t.Errorf("primary physical size = %dx%d, want 344x194", primary.physicalWidth, primary.physicalHeight)
	}

	dummy := outputs[1]
	if dummy.name != "DUMMY0" {
		t.Fatalf("second output = %q, want DUMMY0", dummy.name)
	}
	// The offset is the whole point on Linux: capture is by screen region.
	if dummy.x != 1920 || dummy.y != 0 {
		t.Errorf("dummy origin = %d,%d, want 1920,0", dummy.x, dummy.y)
	}
	if dummy.physicalWidth != 0 {
		t.Errorf("dummy physical width = %d, want 0", dummy.physicalWidth)
	}
}

func TestParseXrandrEmpty(t *testing.T) {
	for name, in := range map[string]string{
		"empty":       "",
		"header":      "Screen 0: minimum 320 x 200, current 1920 x 1080\n",
		"garbage":     "not an xrandr output at all\n",
		"no geometry": "eDP-1 connected (normal left inverted right x axis y axis)\n",
	} {
		if got := parseXrandr([]byte(in)); len(got) != 0 {
			t.Errorf("%s: parsed %d outputs, want 0", name, len(got))
		}
	}
}

func TestPickVirtualOutput(t *testing.T) {
	outputs := parseXrandr([]byte(sampleXrandr))

	// An exact geometry match wins over the 0mm heuristic, so asking for this
	// panel's size finds it even though another 0mm output comes later.
	got, ok := pickVirtualOutput(outputs, 720, 1280)
	if !ok || got.name != "DUMMY0" {
		t.Errorf("pickVirtualOutput(720x1280) = %v %v, want DUMMY0", got.name, ok)
	}

	// Without a size, the 0mm outputs are preferred over real monitors.
	got, ok = pickVirtualOutput(outputs, 0, 0)
	if !ok || got.physicalWidth != 0 {
		t.Errorf("pickVirtualOutput(no size) = %+v, want a 0mm output", got)
	}
	if got.name != "DUMMY0" {
		t.Errorf("pickVirtualOutput(no size) = %q, want the first 0mm output DUMMY0", got.name)
	}
}

func TestPickVirtualOutputNoneFound(t *testing.T) {
	onlyReal := []output{{name: "eDP-1", width: 1920, height: 1080, physicalWidth: 344, physicalHeight: 194}}
	if _, ok := pickVirtualOutput(onlyReal, 720, 1280); ok {
		t.Error("reported a virtual output when only a real monitor is present")
	}
	if _, ok := pickVirtualOutput(nil, 720, 1280); ok {
		t.Error("reported a virtual output from an empty list")
	}
}

// TestPickVirtualOutputGeometryMatchOnRealMonitor documents a deliberate
// trade-off: an exact geometry match wins even if the output is a real monitor.
// A user who asks for a 1920x1080 region on a 1920x1080 screen means that
// screen, so preferring it over the 0mm heuristic is the right call.
func TestPickVirtualOutputGeometryMatchOnRealMonitor(t *testing.T) {
	outputs := parseXrandr([]byte(sampleXrandr))
	got, ok := pickVirtualOutput(outputs, 1920, 1080)
	if !ok || got.name != "eDP-1" {
		t.Errorf("pickVirtualOutput(1920x1080) = %q %v, want eDP-1", got.name, ok)
	}
}
