# turing-monitor

Drive **TURZX / 图灵智显** USB displays from **macOS** and **Linux** — no kernel
driver, no kext signing, no DKMS.

The vendor ships Windows-only software, and its INF targets a USB identity
(`1A86:AD10..AD13`) that a panel in its normal "LCD mode" does not present. This
project talks to the panel directly over USB from userspace.

## Status

| Component | State |
|---|---|
| USB protocol (encryption, framing, commands) | Working, verified against hardware |
| Still images (JPEG) | Working |
| Video (MP4 and raw H.264, streamed) | Working |
| Brightness, rotation, storage info | Working |
| `clear` | Working (via JPEG; the PNG command is unreliable) |
| Live system dashboard | Working (macOS verified; Linux written, untested on hardware) |
| macOS | Working |
| Ubuntu 24.04 | Code is platform-neutral; **not yet verified on real hardware** |
| Extended desktop | Not yet implemented |
| Playback of files stored on the panel | Protocol implemented, blocked on hardware — needs an SD card |

Verified on a TURZX 5.2" panel (`1cbe:0050`, 720×1280) from macOS 26.6.2 on
Apple Silicon.

## Install

Requires Go 1.24+ and **libusb**.

```sh
# macOS
brew install libusb
export PKG_CONFIG_PATH="$(brew --prefix)/lib/pkgconfig"

# Debian / Ubuntu
sudo apt install libusb-1.0-0-dev

go build -o turzx ./cmd/turzx
```

On Linux, install the udev rule so the panel is usable without root:

```sh
sudo cp packaging/linux/99-turzx.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules && sudo udevadm trigger
```

## Usage

```
turzx info                     list attached panels
turzx sync                     handshake with the panel
turzx clear                    blank the screen
turzx image <file>             display a JPEG or PNG, scaled to fit
turzx video <file>             stream an MP4 or raw Annex-B H.264 file
turzx brightness <0-100>       set the backlight
turzx rotate <0-3>             set the display rotation
turzx storage                  report the panel's SD card usage
turzx dashboard                live system monitor, refreshed every second
turzx dashboard --interval 2s  ...at a different refresh rate
```

Examples:

```sh
turzx info
turzx image ~/Pictures/photo.jpg      # any size; scaled and letterboxed
turzx video ~/Movies/clip.mp4         # demuxed to H.264 and streamed
turzx brightness 60
turzx dashboard --interval 1s   # CPU, memory, swap, network, disk, temperatures
```

Images are scaled to the panel's native resolution and letterboxed with black;
the device does not scale. All image payloads are transmitted in the panel's
native **portrait** orientation.

## How it works

Panels in LCD mode expose a single vendor-specific USB interface with two bulk
endpoints (`0x01` OUT, `0x81` IN). The host sends 512-byte blocks that are
DES-CBC encrypted with a fixed key, carrying a 500-byte command packet; payloads
such as JPEG images or H.264 chunks follow the block.

The full wire format — including the golden test vector that pins the encryption
down — is documented in **[docs/PROTOCOL.md](docs/PROTOCOL.md)**.

## Layout

```
cmd/turzx/         command-line interface
internal/proto/    wire protocol: encryption, framing, commands (pure Go)
internal/usb/      USB transport and high-level panel operations
internal/media/    MP4 -> Annex-B H.264 demuxer (pure Go, no ffmpeg needed)
internal/metrics/  system statistics, per platform (procfs, or Mach host stats)
internal/dashboard/ renders metrics to a frame and pushes it
docs/PROTOCOL.md   the reverse-engineered protocol reference
```

## Tests

```sh
go test ./...
```

To exercise the MP4 demuxer against a real file:

```sh
TURZX_TEST_MP4=/path/to/video.mp4 go test ./internal/media/ -v
```

## Notes and limitations

* The panel has **two USB modes**. This project targets LCD mode (`1cbe:00xx`),
  which is what the panel boots into. The vendor's IddCx driver instead uses
  desktop mode (`1a86:AD1x`), which is a different protocol and is not
  implemented here.
* A single USB transfer must stay below 1 MiB or the device times out; larger
  payloads are split into chunks.
* This is an unofficial, reverse-engineered implementation. It is not
  affiliated with TURZX.

## Credits

The protocol was independently confirmed against
[`turing-smart-screen-python`](https://github.com/mathoudebine/turing-smart-screen-python),
whose reference implementation for this device family was invaluable.
