// Package usb talks to TURZX panels over USB.
//
// The panels expose a single vendor-specific interface with two bulk
// endpoints, so no kernel driver is involved on any platform: the interface is
// claimed directly from userspace via libusb. See docs/PROTOCOL.md.
package usb

import (
	"errors"
	"fmt"
	"runtime"
	"sort"
	"sync"

	"github.com/google/gousb"

	"github.com/jijiechen/turing-monitor/internal/proto"
)

const (
	// InterfaceNum is the only interface the panel exposes.
	InterfaceNum = 0
	// EndpointNum addresses both bulk endpoints: OUT 0x01 and IN 0x81.
	EndpointNum = 1
	// ReadSize is the reply buffer size; the device replies in 512-byte blocks.
	ReadSize = 512
)

// ErrNotFound is returned when no supported panel is attached.
var ErrNotFound = errors.New("no TURZX display found (looking for a 0x1cbe device in LCD mode)")

// Info describes an attached panel without claiming it.
type Info struct {
	Bus     int
	Address int
	Serial  string
	Model   proto.Model
}

func (i Info) String() string {
	return fmt.Sprintf("%s  serial %s  bus %d addr %d", i.Model, i.Serial, i.Bus, i.Address)
}

// Discover lists attached panels without claiming any interface. It is safe to
// call while another process is driving a panel.
func Discover() ([]Info, error) {
	ctx := gousb.NewContext()
	defer ctx.Close()

	devs, err := ctx.OpenDevices(matchPanel)
	if err != nil {
		return nil, fmt.Errorf("enumerate USB devices: %w", err)
	}
	defer func() {
		for _, d := range devs {
			d.Close()
		}
	}()

	infos := make([]Info, 0, len(devs))
	for _, d := range devs {
		model, _ := proto.LookupModel(uint16(d.Desc.Product))
		serial, err := d.SerialNumber()
		if err != nil {
			serial = ""
		}
		infos = append(infos, Info{
			Bus:     d.Desc.Bus,
			Address: d.Desc.Address,
			Serial:  serial,
			Model:   model,
		})
	}
	sort.Slice(infos, func(a, b int) bool { return infos[a].Address < infos[b].Address })
	return infos, nil
}

// Open claims the first attached panel and prepares its endpoints for use.
// Only one process can hold a panel open at a time.
func Open() (*Device, error) {
	ctx := gousb.NewContext()

	devs, err := ctx.OpenDevices(matchPanel)
	if err != nil {
		ctx.Close()
		return nil, fmt.Errorf("enumerate USB devices: %w", err)
	}
	if len(devs) == 0 {
		ctx.Close()
		return nil, ErrNotFound
	}

	// Keep the first panel; release any others so we do not hold interfaces we
	// never use.
	dev := devs[0]
	for _, extra := range devs[1:] {
		extra.Close()
	}

	// On Linux the kernel may have bound a driver to the interface, so ask
	// libusb to detach it. On macOS there is nothing to detach and the request
	// fails with LIBUSB_ERROR_ACCESS, so only do this where it is meaningful.
	// The panel's vendor-specific (0xFF) interface is normally unbound, but a
	// distro module could still have claimed it.
	if runtime.GOOS == "linux" {
		if err := dev.SetAutoDetach(true); err != nil {
			dev.Close()
			ctx.Close()
			return nil, fmt.Errorf("enable auto-detach: %w", err)
		}
	}

	model, ok := proto.LookupModel(uint16(dev.Desc.Product))
	if !ok {
		dev.Close()
		ctx.Close()
		return nil, fmt.Errorf("unsupported product ID %#04x", uint16(dev.Desc.Product))
	}

	cfg, err := dev.Config(1)
	if err != nil {
		dev.Close()
		ctx.Close()
		return nil, fmt.Errorf("select configuration: %w", err)
	}
	intf, err := cfg.Interface(InterfaceNum, 0)
	if err != nil {
		cfg.Close()
		dev.Close()
		ctx.Close()
		return nil, fmt.Errorf("claim interface %d: %w", InterfaceNum, err)
	}
	out, err := intf.OutEndpoint(EndpointNum)
	if err != nil {
		intf.Close()
		cfg.Close()
		dev.Close()
		ctx.Close()
		return nil, fmt.Errorf("open bulk OUT endpoint: %w", err)
	}
	in, err := intf.InEndpoint(EndpointNum)
	if err != nil {
		intf.Close()
		cfg.Close()
		dev.Close()
		ctx.Close()
		return nil, fmt.Errorf("open bulk IN endpoint: %w", err)
	}

	serial, err := dev.SerialNumber()
	if err != nil {
		serial = ""
	}

	return &Device{
		ctx:    ctx,
		dev:    dev,
		cfg:    cfg,
		intf:   intf,
		out:    out,
		in:     in,
		model:  model,
		serial: serial,
	}, nil
}

// matchPanel reports whether a device is a supported panel. In LCD mode the
// panels use Ingenic's vendor ID; the vendor's Windows INF targets the
// unrelated desktop-mode identity (0x1a86) instead.
func matchPanel(desc *gousb.DeviceDesc) bool {
	if desc.Vendor != proto.VendorID {
		return false
	}
	_, known := proto.LookupModel(uint16(desc.Product))
	return known
}

// Device is an open panel.
type Device struct {
	ctx    *gousb.Context
	dev    *gousb.Device
	cfg    *gousb.Config
	intf   *gousb.Interface
	out    *gousb.OutEndpoint
	in     *gousb.InEndpoint
	model  proto.Model
	serial string

	// mu guards the endpoints, which do not support concurrent use.
	mu sync.Mutex
}

// Model returns the panel variant.
func (d *Device) Model() proto.Model { return d.model }

// Serial returns the panel's USB serial number, which is stable per unit.
func (d *Device) Serial() string { return d.serial }

// Close releases the panel and its USB resources.
func (d *Device) Close() error {
	if d.intf != nil {
		d.intf.Close()
	}
	if d.cfg != nil {
		d.cfg.Close()
	}
	if d.dev != nil {
		d.dev.Close()
	}
	if d.ctx != nil {
		d.ctx.Close()
	}
	return nil
}
