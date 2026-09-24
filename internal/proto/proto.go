// Package proto implements the TURZX USB display wire protocol.
//
// This is an independent implementation of the protocol these devices speak,
// not published or endorsed by the vendor. It was confirmed byte-for-byte
// against the turing-smart-screen-python project, which supports the same
// 0x1cbe device family. See docs/PROTOCOL.md.
//
// Every host-to-device message is a 512-byte block:
//
//	block[0:504]  DES-CBC(PKCS7(packet), key = IV = "slv3tuzx")
//	block[510]    0xA1
//	block[511]    0x1A
//
// where packet is the 500-byte inner command packet:
//
//	packet[0]     command ID
//	packet[2:4]   0x1A, 0x6D           (magic)
//	packet[4:8]   milliseconds since local midnight, little-endian
//	packet[8:12]  payload length, big-endian      (payload-carrying commands)
//	packet[12]    1 on the final chunk            (H.264 streaming only)
//
// Payload-carrying messages append the raw payload after the block, so the
// bytes written to the bulk OUT endpoint are exactly:
//
//	block ++ payload
package proto

import (
	"crypto/cipher"
	"crypto/des"
	"encoding/binary"
	"fmt"
	"time"
)

const (
	// PacketSize is the size of the inner command packet before encryption.
	PacketSize = 500
	// BlockSize is the size of an encrypted block on the wire.
	BlockSize = 512

	// cryptoKey doubles as both the DES key and the CBC IV. This is a fixed
	// constant of the protocol, not a secret.
	cryptoKey = "slv3tuzx"

	// Magic bytes at packet[2:4].
	magic0 = 0x1A
	magic1 = 0x6D

	// Trailer bytes at block[510:512].
	trailer0 = 0xA1
	trailer1 = 0x1A

	// MaxPayload is the largest payload the device accepts in one transfer.
	// Larger transfers time out.
	MaxPayload = 1 << 20
)

// Packet is the 500-byte inner command packet.
type Packet struct {
	b [PacketSize]byte
}

// NewPacket starts a command packet for cmd, stamped with t.
func NewPacket(cmd byte, t time.Time) *Packet {
	p := &Packet{}
	p.b[0] = cmd
	p.b[2] = magic0
	p.b[3] = magic1
	binary.LittleEndian.PutUint32(p.b[4:8], uint32(MillisSinceMidnight(t)))
	return p
}

// SetByte sets a single byte of the packet body.
func (p *Packet) SetByte(off int, v byte) { p.b[off] = v }

// SetUint32BE sets a big-endian uint32 at off.
func (p *Packet) SetUint32BE(off int, v uint32) { binary.BigEndian.PutUint32(p.b[off:off+4], v) }

// Bytes returns the raw 500-byte packet.
func (p *Packet) Bytes() []byte { return p.b[:] }

// Seal encrypts the packet and frames it into a 512-byte wire block.
func (p *Packet) Seal() ([]byte, error) {
	block, err := des.NewCipher([]byte(cryptoKey))
	if err != nil {
		return nil, fmt.Errorf("des: %w", err)
	}

	plain := pkcs7Pad(p.b[:], block.BlockSize())

	// The IV is the key itself.
	iv := []byte(cryptoKey)
	out := make([]byte, BlockSize)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out[:len(plain)], plain)
	out[BlockSize-2] = trailer0
	out[BlockSize-1] = trailer1
	return out, nil
}

// MillisSinceMidnight returns the protocol's timestamp field: whole
// milliseconds elapsed since local midnight.
func MillisSinceMidnight(t time.Time) int64 {
	y, m, d := t.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	return t.Sub(midnight).Milliseconds()
}

// Command seals a payload-free command into a 512-byte block.
func Command(cmd byte, t time.Time) ([]byte, error) {
	return NewPacket(cmd, t).Seal()
}

// Upload seals cmd and appends payload, giving the full byte string to write to
// the device. Used for image uploads (CmdUploadJPEG, CmdUploadPNG) and H.264
// chunks (CmdPlayH264Chunk).
func Upload(cmd byte, payload []byte, t time.Time) ([]byte, error) {
	if len(payload) > MaxPayload {
		return nil, fmt.Errorf("payload %d bytes exceeds device limit of %d", len(payload), MaxPayload)
	}
	p := NewPacket(cmd, t)
	p.SetUint32BE(8, uint32(len(payload)))
	block, err := p.Seal()
	if err != nil {
		return nil, err
	}
	return append(block, payload...), nil
}

// H264Chunk seals one H.264 stream chunk. final marks the last chunk of the
// stream, which tells the device to close out the playback session.
func H264Chunk(chunk []byte, final bool, t time.Time) ([]byte, error) {
	if len(chunk) > MaxPayload {
		return nil, fmt.Errorf("chunk %d bytes exceeds device limit of %d", len(chunk), MaxPayload)
	}
	p := NewPacket(CmdPlayH264Chunk, t)
	p.SetUint32BE(8, uint32(len(chunk)))
	if final {
		p.SetByte(12, 1)
	}
	block, err := p.Seal()
	if err != nil {
		return nil, err
	}
	return append(block, chunk...), nil
}

// Brightness seals the brightness command. level is in the device's native
// 0-102 range; use BrightnessPercent to convert from a percentage.
func Brightness(level byte, t time.Time) ([]byte, error) {
	p := NewPacket(CmdBrightness, t)
	p.SetByte(8, level)
	return p.Seal()
}

// BrightnessPercent converts a 0-100 percentage to the device's 0-102 scale.
func BrightnessPercent(percent int) (byte, error) {
	if percent < 0 || percent > 100 {
		return 0, fmt.Errorf("brightness %d out of range 0-100", percent)
	}
	return byte(percent * 102 / 100), nil
}

// FrameRate seals the frame-rate command. fps is in frames per second.
func FrameRate(fps byte, t time.Time) ([]byte, error) {
	p := NewPacket(CmdFrameRate, t)
	p.SetByte(8, fps)
	return p.Seal()
}

// Rotate seals the rotation command. Rotation values are 0-3.
func Rotate(rotation byte, t time.Time) ([]byte, error) {
	if rotation > 3 {
		return nil, fmt.Errorf("rotation %d out of range 0-3", rotation)
	}
	p := NewPacket(CmdRotate, t)
	p.SetByte(8, rotation)
	return p.Seal()
}

// Settings is the payload of the save-settings command (CmdSaveSettings).
type Settings struct {
	Brightness byte
	Startup    byte
	Reserved   byte
	Rotation   byte
	Sleep      byte
	Offline    byte
}

// SaveSettings seals the save-settings command.
func SaveSettings(s Settings, t time.Time) ([]byte, error) {
	p := NewPacket(CmdSaveSettings, t)
	p.SetByte(8, s.Brightness)
	p.SetByte(9, s.Startup)
	p.SetByte(10, s.Reserved)
	p.SetByte(11, s.Rotation)
	p.SetByte(12, s.Sleep)
	p.SetByte(13, s.Offline)
	return p.Seal()
}

// Storage describes the reply to CmdStorageInfo.
type Storage struct {
	Total int64
	Used  int64
	Valid int64
}

// ParseStorage decodes a CmdStorageInfo reply.
func ParseStorage(resp []byte) (Storage, error) {
	if len(resp) < 20 {
		return Storage{}, fmt.Errorf("storage reply too short: %d bytes", len(resp))
	}
	return Storage{
		Total: int64(binary.LittleEndian.Uint32(resp[8:12])),
		Used:  int64(binary.LittleEndian.Uint32(resp[12:16])),
		Valid: int64(binary.LittleEndian.Uint32(resp[16:20])),
	}, nil
}

// ParseChunkSize decodes the H.264 chunk size negotiated by CmdGetH264ChunkSize.
// It returns fallback when the device does not report a usable value.
func ParseChunkSize(resp []byte, fallback int) int {
	if len(resp) < 12 {
		return fallback
	}
	n := int(binary.BigEndian.Uint32(resp[8:12]))
	if n <= 0 || n > MaxPayload {
		return fallback
	}
	return n
}

// ParseQueueDepth decodes the stream queue depth reported by CmdGetStreamStatus.
func ParseQueueDepth(resp []byte) int {
	if len(resp) < 9 {
		return 0
	}
	return int(resp[8])
}

// IsAck reports whether a device reply indicates success. The device signals
// acknowledgement with 0xC8 at either reply[1] or reply[8], depending on the
// command.
func IsAck(resp []byte) bool {
	if len(resp) > 1 && resp[1] == 0xC8 {
		return true
	}
	return len(resp) > 8 && resp[8] == 0xC8
}

func pkcs7Pad(b []byte, blockSize int) []byte {
	n := blockSize - len(b)%blockSize
	out := make([]byte, len(b)+n)
	copy(out, b)
	for i := len(b); i < len(out); i++ {
		out[i] = byte(n)
	}
	return out
}
