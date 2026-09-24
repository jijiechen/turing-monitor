package proto

import "fmt"

// VendorID is the USB vendor ID shared by this family of panels. In LCD mode
// the panels use Ingenic's vendor ID rather than WCH's (0x1A86), which is what
// the vendor's Windows INF targets; that INF covers the separate "desktop mode"
// identity instead.
const VendorID = 0x1cbe

// Model describes one panel variant.
type Model struct {
	PID    uint16
	Name   string
	Width  int // portrait width
	Height int // portrait height
}

// Portrait returns the panel's native (unrotated) dimensions. All image
// payloads are sent in this orientation; rotate on the host for landscape.
func (m Model) Portrait() (int, int) { return m.Width, m.Height }

func (m Model) String() string {
	return fmt.Sprintf("%s (%dx%d)", m.Name, m.Width, m.Height)
}

// Models is the table of known panels keyed by product ID. Dimensions are the
// portrait orientation, as reported by the reference implementation.
var Models = map[uint16]Model{
	0x0028: {PID: 0x0028, Name: `Turing 2.8" round`, Width: 480, Height: 480},
	0x0046: {PID: 0x0046, Name: `Turing 4.6"`, Width: 320, Height: 960},
	0x0050: {PID: 0x0050, Name: `Turing 5.2"`, Width: 720, Height: 1280},
	0x0080: {PID: 0x0080, Name: `Turing 8.0"`, Width: 800, Height: 1280},
	0x0088: {PID: 0x0088, Name: `Turing 8.8"`, Width: 480, Height: 1920},
	0x0092: {PID: 0x0092, Name: `Turing 9.2"`, Width: 462, Height: 1920},
	0x0123: {PID: 0x0123, Name: `Turing 12.3"`, Width: 720, Height: 1920},
}

// LookupModel returns the model for a product ID.
func LookupModel(pid uint16) (Model, bool) {
	m, ok := Models[pid]
	return m, ok
}

// ProductIDs returns every known product ID, for device scanning.
func ProductIDs() []uint16 {
	ids := make([]uint16, 0, len(Models))
	for id := range Models {
		ids = append(ids, id)
	}
	return ids
}
