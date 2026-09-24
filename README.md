# turing-monitor

Drive **TURZX / 图灵智显** USB displays from **macOS** and **Linux** — no kernel
driver, no kext signing, no DKMS.

The vendor only ships Windows software, and its driver targets a USB identity
(`1A86:AD10..AD13`) that a panel in its normal "LCD mode" does not present. So
on macOS and Linux the panel is simply unrecognised. This project talks to it
directly from userspace, which works on both systems with nothing installed
beyond libusb.

What you can do with it:

- **Show an image** — any JPEG or PNG, scaled to the panel
- **Play a video** — MP4 or raw H.264
- **Run a system dashboard** — CPU, memory, network, disk and temperatures
- **Use it as a second monitor** — drag real windows onto it

## Supported displays

| USB ID | Model | Resolution |
|---|---|---|
| `1cbe:0050` | Turing 5.2" | 720×1280 |
| `1cbe:0028` | Turing 2.8" round | 480×480 |
| `1cbe:0046` | Turing 4.6" | 320×960 |
| `1cbe:0080` | Turing 8.0" | 800×1280 |
| `1cbe:0088` | Turing 8.8" | 480×1920 |
| `1cbe:0092` | Turing 9.2" | 462×1920 |
| `1cbe:0123` | Turing 12.3" | 720×1920 |

Run `turzx info` to see what your panel reports. If it is not listed, the model
table in `internal/proto/model.go` is one line to extend.

## Install

### macOS

```sh
brew install libusb
git clone https://github.com/jijiechen/turing-monitor
cd turing-monitor
PKG_CONFIG_PATH="$(brew --prefix)/lib/pkgconfig" go build -o turzx ./cmd/turzx
```

Requires Go 1.24 or newer. If you would rather not install Go, download a
prebuilt binary from [Releases](../../releases) instead — macOS builds are
unsigned, so the first run needs **right-click → Open**, or:

```sh
xattr -d com.apple.quarantine ./turzx
```

### Ubuntu / Debian

```sh
sudo apt install libusb-1.0-0-dev
sudo apt install ffmpeg          # only needed for `turzx monitor`
git clone https://github.com/jijiechen/turing-monitor
cd turing-monitor
go build -o turzx ./cmd/turzx
```

Then let your user talk to the panel without `sudo`:

```sh
sudo cp packaging/linux/99-turzx.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules && sudo udevadm trigger
```

Unplug and replug the panel. Without this step every command fails with
`libusb: bad access`.

## Usage

Check the panel is found first:

```sh
./turzx info
# Turing 5.2" (720x1280)  serial 2e1fe5abea900204  bus 2 addr 1
```

### Show an image

```sh
./turzx image ~/Pictures/photo.jpg
```

Any size is accepted: the image is scaled to fit the panel and letterboxed with
black. The panel does not scale anything itself, so the host does all fitting.

### Play a video

```sh
./turzx video ~/Movies/clip.mp4          # MP4 is demuxed on the fly
./turzx video clip.h264 --loop           # raw Annex-B H.264, repeating
./turzx video clip.mp4 --fps 24          # override the frame rate
```

MP4 handling is built in, so **ffmpeg is not needed** for this. Playback is
paced by the panel, which means the whole clip is uploaded in a fraction of its
duration and then keeps playing — that is expected.

### System dashboard

```sh
./turzx dashboard                        # refresh every second
./turzx dashboard --interval 500ms
```

Shows CPU, load average, memory, swap, network throughput, disk usage and
temperature. Press Ctrl-C to stop.

macOS reports no temperature: Apple Silicon exposes it only through the SMC,
which needs access an unsigned command-line tool does not have. The layout
adapts and gives the space to the disk panel instead.

### Second monitor

```sh
./turzx monitor                  # 30 fps, 16 Mbit/s
./turzx monitor --bitrate 24000  # sharper motion, if your panel keeps up
./turzx monitor --fps 24         # fewer frames, if it does not
```

See **[Extended desktop](#extended-desktop)** below — this one needs setup on
Linux, and a permission on macOS.

### Other commands

```sh
./turzx brightness 60     # backlight, 0-100
./turzx rotate 1          # 0-3, for a panel mounted sideways
./turzx clear             # blank the screen
./turzx sync              # handshake; also prints the firmware version
./turzx storage           # SD card usage, if a card is inserted
./turzx extract in.mp4 out.h264   # convert without touching the panel
```

## Extended desktop

`turzx monitor` creates a display the size of the panel, captures it, encodes
H.264 and streams it, so the panel becomes a real second screen you can drag
windows onto.

### macOS

Permission is needed once: **System Settings → Privacy & Security → Screen
Recording**, enable your terminal, then restart the terminal. macOS prompts for
this on the first run, and without it capture fails with a message saying so.

The virtual display is created by this program using the private
`CGVirtualDisplay` API inside CoreGraphics — the same mechanism DisplayLink,
BetterDisplay and DeskPad use. Two consequences worth knowing:

- It is **undocumented**, so a macOS update may break it.
- It cannot be shipped on the Mac App Store.

### Linux

Two requirements:

**1. An X11 session.** Log in choosing "Ubuntu on Xorg". Wayland is not
supported: its compositors offer no way to create a virtual output, and screen
capture goes through a portal rather than the X screen.

**2. A virtual output that already exists.** A userspace program cannot create
a display on Linux, so set one up first. Either install evdi:

```sh
sudo apt install evdi-dkms && sudo modprobe evdi
```

or add a dummy device to `xorg.conf`:

```
Section "Device"
  Identifier "Virtual"
  Driver     "dummy"
  VideoRam   32768
  Option     "ConnectedMonitor" "DP-1"
EndSection
```

Then run `turzx monitor`. It picks the output automatically, preferring one
already sized to the panel. If you have several, choose with
`--output <name>` — names come from `xrandr --query`. If it cannot find one it
tells you what is available and what to install.

### Tuning

The defaults target desktop content at 720×1280 over USB 2.0. If motion looks
blocky, raise `--bitrate`; if it stutters, lower `--fps`. Note that a static
screen produces almost no traffic — the panel keeps showing the last frame —
so it is normal for the frame rate to sit low until something moves.

## Troubleshooting

**`no TURZX display found`**
The panel is either unplugged or in desktop mode. Check `lsusb` (Linux) or
System Information → USB (macOS) for a `1cbe:` device. If you see `1a86:ad1x`
instead, the panel is in desktop mode, which this project does not implement.

**`libusb: bad access` / `claim interface failed`**
On Linux, the udev rule above is missing. On either system, another copy of
`turzx` may still be running and holding the panel — only one process can use
it at a time.

**`claim interface 0` fails right after killing a previous run**
The OS needs a moment to release the interface. Wait a few seconds.

**Extended desktop: `Screen Recording permission is not granted`**
See the macOS section above; the terminal must be restarted after granting it.

**Extended desktop: motion looks blocky or smeared**
Raise `--bitrate`. If it is still blocky, note that the panel's hardware
decoder only supports H.264 4:2:0, so fine coloured text keeps slight colour
fringing — that part is not tunable.

**Video plays much faster than it should**
Playback is paced by the panel from its own buffer. If the panel ignores the
requested frame rate, pass `--fps` explicitly.

## Status

| | macOS | Linux |
|---|---|---|
| Images, video, brightness, rotate | ✅ verified | ⚠️ untested |
| System dashboard | ✅ verified | ⚠️ untested |
| Extended desktop | ✅ verified | ⚠️ untested |

macOS has been exercised against a real 5.2" panel on macOS 26.6.2 (Apple
Silicon). **The Linux paths are implemented but have not yet been run on real
hardware** — expect to fix things, and reports are welcome.

## How it works

Panels in LCD mode expose a single vendor-specific USB interface with two bulk
endpoints (`0x01` OUT, `0x81` IN). Every message is a 512-byte block: a
DES-CBC-encrypted 500-byte command packet, optionally followed by a raw payload
such as a JPEG image or a chunk of H.264.

The full wire format — including the golden test vector that pins the
encryption down — is documented in **[docs/PROTOCOL.md](docs/PROTOCOL.md)**.

## Layout

```
cmd/turzx/          command-line interface
internal/proto/     wire protocol: encryption, framing, commands (pure Go)
internal/usb/       USB transport and high-level panel operations
internal/media/     MP4 -> Annex-B H.264 demuxer (pure Go, no ffmpeg needed)
internal/metrics/   system statistics, per platform
internal/dashboard/ renders metrics to a frame and pushes it
internal/vdisplay/  creates or finds a virtual display
internal/capture/   captures a display and encodes it to H.264
docs/PROTOCOL.md    the reverse-engineered protocol reference
```

## Development

```sh
go test ./...
```

To exercise the MP4 demuxer against a real file:

```sh
TURZX_TEST_MP4=/path/to/video.mp4 go test ./internal/media/ -v
```

## Notes and limitations

* The panel has **two USB modes**. This project targets **LCD mode**
  (`1cbe:00xx`), which is what the panel boots into. The vendor's Windows
  driver instead uses **desktop mode** (`1a86:AD1x`), a different protocol that
  is not implemented here.
* A single USB transfer must stay below 1 MiB or the device times out; larger
  payloads are split automatically.
* Playback of files stored on the panel's own SD card is implemented at the
  protocol level but needs a card inserted, and is untested.
* This is an **unofficial, reverse-engineered** implementation. It is not
  affiliated with TURZX.

## Credits

The protocol was independently confirmed against
[`turing-smart-screen-python`](https://github.com/mathoudebine/turing-smart-screen-python),
whose reference implementation for this device family was invaluable.

## License

MIT — see [LICENSE](LICENSE).
