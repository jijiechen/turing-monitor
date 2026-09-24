package proto

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

// midnight is a time whose MillisSinceMidnight is 0, making the packet's
// timestamp field deterministic.
var midnight = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// Golden vectors that lock the encryption, padding and framing in place. They
// were cross-checked against the turing-smart-screen-python reference
// implementation for the same device family, so an implementation reproducing
// them is byte-compatible with a known-good one.
func TestSealGoldenVectors(t *testing.T) {
	tests := []struct {
		name    string
		cmd     byte
		size    uint32 // 0 means "leave the length field zero"
		wantSum string
		wantHex string // first 16 bytes
	}{
		{
			name:    "upload-jpeg of a 26991 byte image",
			cmd:     CmdUploadJPEG,
			size:    26991,
			wantSum: "cb2a2880c5fb65945b4fc0eced71a9bfc83c858a8adc58ff9ee602fa3fab8d2c",
			wantHex: "96cd0b62e7370d33e99363c99eb3a3f5",
		},
		{
			name:    "sync command",
			cmd:     CmdSync,
			wantHex: "3e7d4dc507d9a2156af8c661aba0a30e",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := NewPacket(tc.cmd, midnight)
			if tc.size != 0 {
				p.SetUint32BE(8, tc.size)
			}
			block, err := p.Seal()
			if err != nil {
				t.Fatalf("Seal: %v", err)
			}
			if len(block) != BlockSize {
				t.Fatalf("block length = %d, want %d", len(block), BlockSize)
			}
			if got := hex.EncodeToString(block[:16]); got != tc.wantHex {
				t.Errorf("first 16 bytes\n got %s\nwant %s", got, tc.wantHex)
			}
			if tc.wantSum != "" {
				sum := sha256.Sum256(block)
				if got := hex.EncodeToString(sum[:]); got != tc.wantSum {
					t.Errorf("sha256\n got %s\nwant %s", got, tc.wantSum)
				}
			}
		})
	}
}

func TestSealTrailer(t *testing.T) {
	block, err := Command(CmdSync, midnight)
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if block[510] != 0xA1 || block[511] != 0x1A {
		t.Errorf("trailer = %#x %#x, want 0xa1 0x1a", block[510], block[511])
	}
	// DES-CBC over a PKCS7-padded 500-byte packet yields 504 ciphertext bytes;
	// the remaining bytes up to the trailer stay zero.
	if !bytes.Equal(block[504:510], make([]byte, 6)) {
		t.Errorf("bytes 504:510 = % x, want zero padding", block[504:510])
	}
}

func TestSealIsDeterministic(t *testing.T) {
	a, err := Command(CmdSync, midnight)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Command(CmdSync, midnight)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("sealing the same packet twice produced different output")
	}
}

func TestUploadAppendsPayload(t *testing.T) {
	payload := []byte("hello turzx")
	got, err := Upload(CmdUploadJPEG, payload, midnight)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if len(got) != BlockSize+len(payload) {
		t.Fatalf("length = %d, want %d", len(got), BlockSize+len(payload))
	}
	if !bytes.Equal(got[BlockSize:], payload) {
		t.Errorf("payload = %q, want %q", got[BlockSize:], payload)
	}
	if got[510] != 0xA1 || got[511] != 0x1A {
		t.Errorf("trailer = %#x %#x, want 0xa1 0x1a", got[510], got[511])
	}
}

func TestUploadRejectsOversizePayload(t *testing.T) {
	if _, err := Upload(CmdUploadJPEG, make([]byte, MaxPayload+1), midnight); err == nil {
		t.Error("expected an error for an oversize payload")
	}
}

func TestH264ChunkFinalFlag(t *testing.T) {
	// The final-chunk marker lives inside the encrypted packet, so verify it
	// round-trips rather than reading it off the wire block.
	mk := func(final bool) []byte {
		p := NewPacket(CmdPlayH264Chunk, midnight)
		p.SetUint32BE(8, 4)
		if final {
			p.SetByte(12, 1)
		}
		return p.Bytes()
	}
	if bytes.Equal(mk(true), mk(false)) {
		t.Error("final and non-final chunks produced identical packets")
	}

	chunk, err := H264Chunk([]byte{0, 0, 0, 1}, true, midnight)
	if err != nil {
		t.Fatalf("H264Chunk: %v", err)
	}
	if len(chunk) != BlockSize+4 {
		t.Errorf("length = %d, want %d", len(chunk), BlockSize+4)
	}
}

func TestMillisSinceMidnight(t *testing.T) {
	noon := time.Date(2024, 3, 5, 12, 0, 0, 0, time.UTC)
	if got, want := MillisSinceMidnight(noon), int64(12*60*60*1000); got != want {
		t.Errorf("MillisSinceMidnight = %d, want %d", got, want)
	}
	if got := MillisSinceMidnight(midnight); got != 0 {
		t.Errorf("MillisSinceMidnight(midnight) = %d, want 0", got)
	}
	// Local midnight must be 0 regardless of the zone offset.
	loc := time.FixedZone("UTC+8", 8*60*60)
	local := time.Date(2024, 3, 5, 0, 0, 0, 0, loc)
	if got := MillisSinceMidnight(local); got != 0 {
		t.Errorf("MillisSinceMidnight(local midnight) = %d, want 0", got)
	}
}

func TestBrightnessPercent(t *testing.T) {
	tests := []struct {
		percent int
		want    byte
	}{
		{0, 0}, {50, 51}, {100, 102},
	}
	for _, tc := range tests {
		got, err := BrightnessPercent(tc.percent)
		if err != nil {
			t.Fatalf("BrightnessPercent(%d): %v", tc.percent, err)
		}
		if got != tc.want {
			t.Errorf("BrightnessPercent(%d) = %d, want %d", tc.percent, got, tc.want)
		}
	}
	for _, bad := range []int{-1, 101} {
		if _, err := BrightnessPercent(bad); err == nil {
			t.Errorf("BrightnessPercent(%d): expected an error", bad)
		}
	}
}

func TestRotateRejectsOutOfRange(t *testing.T) {
	if _, err := Rotate(4, midnight); err == nil {
		t.Error("expected an error for rotation 4")
	}
	if _, err := Rotate(3, midnight); err != nil {
		t.Errorf("Rotate(3): %v", err)
	}
}

func TestIsAck(t *testing.T) {
	tests := []struct {
		name string
		resp []byte
		want bool
	}{
		{"ack at index 1", []byte{0x65, 0xC8, 0x94, 0x38, 0x99, 0x02, 0, 0, 0x00}, true},
		{"ack at index 8", append(make([]byte, 8), 0xC8), true},
		{"no ack", make([]byte, 12), false},
		{"short reply", []byte{0x01}, false},
		{"empty reply", nil, false},
	}
	for _, tc := range tests {
		if got := IsAck(tc.resp); got != tc.want {
			t.Errorf("%s: IsAck = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestParseChunkSize(t *testing.T) {
	resp := make([]byte, 12)
	resp[8], resp[9], resp[10], resp[11] = 0x00, 0x03, 0x18, 0x00 // 202752
	if got := ParseChunkSize(resp, 4096); got != 202752 {
		t.Errorf("ParseChunkSize = %d, want 202752", got)
	}
	if got := ParseChunkSize([]byte{1, 2}, 4096); got != 4096 {
		t.Errorf("short reply: got %d, want fallback 4096", got)
	}
	// A zero or absurd value must fall back rather than be trusted.
	if got := ParseChunkSize(make([]byte, 12), 4096); got != 4096 {
		t.Errorf("zero chunk size: got %d, want fallback 4096", got)
	}
}

func TestParseStorage(t *testing.T) {
	resp := make([]byte, 20)
	for i, v := range []uint32{1000, 250, 900} {
		putLE(resp[8+i*4:], v)
	}
	s, err := ParseStorage(resp)
	if err != nil {
		t.Fatalf("ParseStorage: %v", err)
	}
	if s.Total != 1000 || s.Used != 250 || s.Valid != 900 {
		t.Errorf("got %+v, want {1000 250 900}", s)
	}
	if _, err := ParseStorage(make([]byte, 4)); err == nil {
		t.Error("expected an error for a short reply")
	}
}

func TestLookupModel(t *testing.T) {
	m, ok := LookupModel(0x0050)
	if !ok {
		t.Fatal("0x0050 not found")
	}
	if m.Width != 720 || m.Height != 1280 {
		t.Errorf("got %dx%d, want 720x1280", m.Width, m.Height)
	}
	if _, ok := LookupModel(0xdead); ok {
		t.Error("unexpected model for 0xdead")
	}
}

func putLE(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}
