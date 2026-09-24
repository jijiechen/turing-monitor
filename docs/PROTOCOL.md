# TURZX USB display protocol

This documents the wire protocol used by TURZX / 图灵智显 panels in **LCD mode**
(USB `1cbe:0050` and siblings). It is an independent implementation, not
published or endorsed by the vendor, and it has been confirmed byte-for-byte
against the
[`turing-smart-screen-python`](https://github.com/mathoudebine/turing-smart-screen-python)
reference implementation. Every claim below has been exercised against physical
hardware.

## Device identification

Panels in LCD mode use **Ingenic's** vendor ID, not WCH's:

| | |
|---|---|
| Vendor ID | `0x1cbe` |
| Product IDs | see the model table below |
| Device class | `0xFF` vendor-specific, subclass `0xFF`, protocol `0xFF` |
| Configuration | 1 configuration, 1 interface (`0`), no alternate settings |
| Endpoints | `0x01` bulk OUT, `0x81` bulk IN — both 512-byte max packet |
| Strings | manufacturer `TURZX`, product `TURZX1.0` |

### Model table

| PID | Model | Resolution (portrait) |
|---|---|---|
| `0x0028` | Turing 2.8" round | 480×480 |
| `0x0046` | Turing 4.6" | 320×960 |
| `0x0050` | Turing 5.2" | 720×1280 |
| `0x0080` | Turing 8.0" | 800×1280 |
| `0x0088` | Turing 8.8" | 480×1920 |
| `0x0092` | Turing 9.2" | 462×1920 |
| `0x0123` | Turing 12.3" | 720×1920 |

### The two modes

The same physical panel re-enumerates with a different USB identity depending on
its mode:

| Mode | USB ID | Driven by |
|---|---|---|
| **LCD mode** | `1cbe:00xx` | the vendor app; JPEG/PNG/H.264 pushed by the host |
| Desktop mode | `1a86:AD1x` | the vendor's IddCx driver (`TURZX_display_driver.dll`) |

The vendor's Windows INF targets `1A86:AD10..AD13`, so it does **not** match a
panel that is sitting in LCD mode. This project targets LCD mode, which is the
mode the panel boots into and which supports H.264 streaming. Desktop mode uses
an unrelated protocol (`0xAF`/`0x20` TLV control packets and `0x6C`/`0x6D`
framing) and is not implemented here.

## Framing

Every host→device message begins with a **512-byte block**:

```
block[0:504]   DES-CBC( PKCS7_pad(packet, 8), key = IV = "slv3tuzx" )
block[504:510] zero
block[510]     0xA1
block[511]     0x1A
```

* Algorithm: DES in CBC mode.
* Key **and** IV: the 8 ASCII bytes `slv3tuzx`. This is a fixed protocol
  constant, not a secret.
* Padding: PKCS#7. A 500-byte packet pads to 504 bytes, which is the ciphertext
  length; the remaining bytes up to the trailer stay zero.

The plaintext is a **500-byte packet**:

| Offset | Size | Meaning |
|---|---|---|
| 0 | 1 | command ID |
| 1 | 1 | unused (zero) |
| 2 | 1 | `0x1A` magic |
| 3 | 1 | `0x6D` magic |
| 4 | 4 | milliseconds since **local midnight**, uint32 little-endian |
| 8 | 4 | payload length, uint32 **big-endian** (payload commands only) |
| 12 | 1 | `1` on the final chunk (H.264 streaming only) |
| 13 | — | zero padding to 500 bytes |

For payload-carrying commands the raw payload follows the block, so the bytes
written to the bulk OUT endpoint are exactly `block || payload`.

## Replies

After each write the host reads one 512-byte block from the IN endpoint and then
drains any stragglers. Success is signalled by **`0xC8` at either `reply[1]` or
`reply[8]`**, depending on the command. Not every command replies.

Several commands return data big- or little-endian at fixed offsets — see the
table below.

## Commands

| ID | Name | Request | Reply |
|---|---|---|---|
| 10 | sync | — | ack |
| 11 | restart | — | ack |
| 13 | rotate | `[8]` = rotation 0–3 | ack |
| 14 | brightness | `[8]` = level 0–102 | ack |
| 15 | frame rate | `[8]` = fps | ack |
| 17 | get H.264 chunk size | — | `[8:12]` uint32 BE |
| 100 | storage info | — | `[8:12]` total, `[12:16]` used, `[16:20]` valid — all uint32 **LE** |
| 101 | upload JPEG | `[8:12]` = length BE, then JPEG bytes | ack |
| 102 | upload PNG | `[8:12]` = length BE, then PNG bytes | ack — **but no visible effect on the 5.2" panel**; prefer 101 |
| 121 | play H.264 chunk | `[8:12]` = length BE, `[12]` = 1 on final, then chunk | ack |
| 122 | stream status | — | `[8]` = playback queue depth |
| 123 | stop stream | — | ack |
| 125 | save settings | `[8]`=brightness `[9]`=startup `[10]`=reserved `[11]`=rotation `[12]`=sleep `[13]`=offline | ack |
| 38 | open file for writing | `[8:12]` = path length BE, path at `[16:]` | ack |
| 39 | write file chunk | `[8:12]` = capacity, `[12:16]` = chunk length (both BE), `[16]`=1 on last chunk, payload at 512 | ack |
| 40 | delete file | `[8:12]` = path length BE, path at `[16:]` | ack |
| 98 | play file | `[8:12]` = path length BE, path at `[16:]` | ack |
| 110 | play file (alternate) | as 98 | ack |
| 113 | play image file | as 98 | ack |

Command 10 (sync) replies with the firmware version string, e.g. `turzx_0001_0015`,
starting at `reply[8]`.

Brightness uses a **0–102** scale, not 0–100; the vendor app maps a percentage
with `level * 102 / 100`.

## Practical constraints

* **Payload limit.** A single transfer must stay below 1 MiB or the device times
  out. Larger payloads must be split into separate chunks.
* **Back-pressure.** During H.264 playback, query command 122 and pause while the
  reported queue depth exceeds ~3, otherwise frames are dropped.
* **Orientation.** Images are transmitted in the panel's **native portrait**
  orientation, and the device does not scale. The host must resize and letterbox;
  rotate host-side for landscape content.
* **Clearing.** The reference implementation clears the screen with a black PNG
  hardcoded to the 8.8" panel's 480×1920 geometry. That is wrong for other
  models — generate the image at the panel's own resolution instead.

## Streaming H.264

The device contains a hardware H.264 decoder, so the host sends an **Annex-B
elementary stream** (start-code delimited NAL units), not a container format:

1. Run the **prelude** below. This is mandatory.
2. Optionally negotiate the chunk size with command 17.
3. Split the Annex-B stream into chunks (≤ negotiated size, < 1 MiB).
4. Send each chunk with command 121, setting `[12] = 1` on the **last** chunk so
   the device closes the session cleanly.
5. Query command 122 between chunks and pause while the queue is full.
6. Send command 123 to stop.

### The streaming prelude is load-bearing

Before any chunks, send these as **bare command headers** (no payload byte), in
this order:

```
111    stop playback already in progress
112    reset the video pipeline
13     (sent bare, without its usual rotation byte)
41
then: a still image  — see below
then: 15 with [8] = frames per second
```

Skipping this prelude produces a confusing failure: the device **accepts every
chunk and the playback queue drains normally**, exactly as if playback were
working, but the screen never changes. It keeps showing whatever was displayed
before, so the stream is silently discarded.

**The clear step must actually take effect.** Commands 111/112/13/41 do not
dislodge the panel from still-image display mode on their own. Sending a fresh
still image does. Use command **101 (JPEG)** — command 102 (PNG) is acknowledged
by the device but produces no visible change on the 5.2" panel, so a prelude
built on a PNG "clear" fails at the last step and streaming appears broken.

Playback is paced by the device, not the host: after uploading, the playback
queue drains at the rate set by command 15. A whole clip is therefore pushed
much faster than it plays, and it keeps playing after the host command returns.

### The device can also play files from its own storage

The vendor Windows application does not stream over USB at all. It uploads the
file to the panel's removable storage and asks the panel to play it locally:

```
38   open file for writing — path at [16:16+len], length at [8:12]
39   write chunk — [8:12] capacity, [12:16] chunk length, [16]=1 on last, payload at 512
98   play file  (also 110 and 113; 113 is used for images)
40   delete file
```

Paths are of the form `/tmp/sdcard/mmcblk0p1/video/<name>.h264`. This requires a
card in the panel's slot: command 100 reports zero capacity with no card
inserted, and uploads then have nowhere to go.

Command 100 returns the card usage in **little-endian** at `[8:12]` total,
`[12:16]` used, `[16:20]` valid.

### Measured throughput

Pushing pre-encoded JPEG stills through command 101 sustains about **8 fps** at
720×1280 (≈30 KB/frame, ≈230 KB/s) on the 5.2" panel. That is the practical
ceiling for still-image animation and is what the dashboard mode uses.

`MP4` files must be converted to Annex-B first (demux `moov`, convert each
sample's length-prefixed NAL units to start codes, and prepend the SPS/PPS from
the `avcC` box).

## Writing settings is dangerous and delayed

**Read this before sending command 125, or any command whose payload fields are
not fully understood.**

Command 125 saves settings to the panel's own storage. Two properties make it
easy to break a working panel with it, and both were learned the hard way on
real hardware:

1. **Every field you do not set is zero.** The packet is zero-initialised, so
   sending only the field you care about silently writes zeros to all the
   others. `[8]` is the backlight level, so setting only `[11]` (rotation) turns
   the backlight off.

2. **It takes effect at the next boot, not immediately.** The screen keeps
   working normally right after a bad write, which makes the write look
   harmless. The damage only appears after the panel is unplugged or rebooted.
   Verifying a settings write by looking at the screen will tell you nothing.

Recovery, in order:

```
14  with [8] = 102     set the backlight directly; this one is immediate
125 with [8] = 102 and every other field 0    write a sane full configuration
11                     reboot, so the corrected settings take effect
```

A bad settings write cannot brick the panel. These are configuration values,
not firmware, and the protocol has no way to write firmware.

### Commands with unknown semantics

`[9]` startup, `[12]` sleep and `[13]` offline in command 125 have no documented
meaning here. Zero is what the reference implementation uses, and is the value
that restored a panel, but that is not the same as knowing what they do. Treat
them as unknown and avoid writing them.

### Rotation does not apply to pushed content

Both `13` and `125` (with a rotation value) were tested against a 5.2" panel
with a still image on screen. **Neither has any visible effect.** Rotation is
therefore not a device-side property of pushed content, and an application that
needs it must rotate on the host. Command 13's bare form is used by the vendor
application as part of the video prelude, which is a different thing.

## Verifying an implementation

The encryption is deterministic, so a golden vector pins it down:

```
command = 101 (upload JPEG), payload length = 26991, timestamp = 0
=> 512-byte block, sha256
   cb2a2880c5fb65945b4fc0eced71a9bfc83c858a8adc58ff9ee602fa3fab8d2c
   first 16 bytes 96 cd 0b 62 e7 37 0d 33 e9 93 63 c9 9e b3 a3 f5
   bytes 510,511 = 0xa1, 0x1a
```

`internal/proto/proto_test.go` asserts exactly this, plus the equivalent vector
for command 10.
