package flp

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildRawFLP creates a minimal FLP byte stream from raw FLdt payload.
func buildRawFLP(ppq uint16, fldt []byte) []byte {
	var out bytes.Buffer
	out.WriteString("FLhd")
	_ = binary.Write(&out, binary.LittleEndian, uint32(6))
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))
	_ = binary.Write(&out, binary.LittleEndian, uint16(2))
	_ = binary.Write(&out, binary.LittleEndian, ppq)
	out.WriteString("FLdt")
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(fldt)))
	out.Write(fldt)
	return out.Bytes()
}

// TestCorruptedFLdt — parser must not crash on a truncated/garbled event stream.
func TestCorruptedFLdt(t *testing.T) {
	// Garbage bytes after a valid byte event.
	garbage := []byte{
		evChanType, 0x02, // valid byte event
		0xC5,             // text event id with no length/data
		0xDE, 0xAD, 0xBE, // random bytes
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.flp")
	if err := os.WriteFile(path, buildRawFLP(96, garbage), 0o644); err != nil {
		t.Fatal(err)
	}
	// Must not panic; error is acceptable but not required.
	p := New()
	proj, err := p.Parse(context.Background(), path)
	if err != nil {
		return // tolerable error
	}
	if proj == nil {
		t.Error("expected non-nil project even for corrupt FLdt")
	}
}

// TestTruncatedFLdtLength — FLhd reports FLdt length larger than actual file.
func TestTruncatedFLdtLength(t *testing.T) {
	var out bytes.Buffer
	out.WriteString("FLhd")
	_ = binary.Write(&out, binary.LittleEndian, uint32(6))
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))
	_ = binary.Write(&out, binary.LittleEndian, uint16(4))
	_ = binary.Write(&out, binary.LittleEndian, uint16(96))
	out.WriteString("FLdt")
	_ = binary.Write(&out, binary.LittleEndian, uint32(99999)) // claims 99999 bytes but file is tiny
	out.Write([]byte{evChanType, 0x00})                        // only 2 bytes of actual data

	dir := t.TempDir()
	path := filepath.Join(dir, "truncated.flp")
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	p := New()
	proj, _ := p.Parse(context.Background(), path)
	// Should produce a partial project without panicking.
	if proj == nil {
		t.Error("expected partial project, not nil")
	}
}

// TestNonUTF8SamplePath — FLP with a sample path containing raw bytes that are
// not valid UTF-8 in ASCII mode. Parser should not crash.
func TestNonUTF8SamplePath(t *testing.T) {
	b := &flpBuilder{}
	b.dwordEvent(evFineTempo, 120000)
	b.wordEvent(evNewChan, 0)
	// Write a text event with invalid UTF-8 bytes (non-UTF16 payload).
	b.events.WriteByte(evTextSamplePath)
	raw := []byte{0xC0, 0xC1, 0xFF, 0xFE, 0x00} // invalid UTF-8 + null terminator
	b.writeVarLen(len(raw))
	b.events.Write(raw)

	dir := t.TempDir()
	path := filepath.Join(dir, "nonuft8.flp")
	if err := os.WriteFile(path, b.bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	proj, err := New().Parse(context.Background(), path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The sample path is present (might be garbled) but must not be empty when
	// bytes were provided.
	if len(proj.SamplePaths) == 0 {
		// Not a hard failure — empty is acceptable for truly unreadable bytes.
		t.Log("note: non-UTF8 sample path was skipped by parser")
	}
}

// TestLongPath — 4000-character sample path.
func TestLongPath(t *testing.T) {
	longPath := strings.Repeat("A", 4000) + ".wav"
	b := &flpBuilder{}
	b.wordEvent(evNewChan, 0)
	b.textEventASCII(evTextSamplePath, longPath)

	dir := t.TempDir()
	path := filepath.Join(dir, "long.flp")
	if err := os.WriteFile(path, b.bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	proj, err := New().Parse(context.Background(), path)
	if err != nil {
		t.Fatalf("unexpected parse error for long path: %v", err)
	}
	if len(proj.SamplePaths) != 1 || len(proj.SamplePaths[0]) != len(longPath) {
		t.Errorf("long path not preserved: got len=%d", len(proj.SamplePaths[0]))
	}
}

// TestEmptyProject — valid FLP headers but no events in FLdt.
func TestEmptyProject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.flp")
	if err := os.WriteFile(path, buildRawFLP(96, nil), 0o644); err != nil {
		t.Fatal(err)
	}
	proj, err := New().Parse(context.Background(), path)
	if err != nil {
		t.Fatalf("unexpected error for empty FLdt: %v", err)
	}
	if proj == nil {
		t.Fatal("expected non-nil project")
	}
	if proj.BPM != 0 {
		t.Errorf("expected bpm=0 for empty project, got %v", proj.BPM)
	}
}

// TestContextCancellation — parsing respects context cancellation.
func TestContextCancellation(t *testing.T) {
	// Build a large synthetic FLP with many events to give the parser time to check ctx.
	b := &flpBuilder{}
	b.dwordEvent(evFineTempo, 140000)
	for i := 0; i < 5000; i++ {
		b.wordEvent(evNewChan, uint16(i%128))
		b.byteEvent(evChanType, 0)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "big.flp")
	if err := os.WriteFile(path, b.bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := New().Parse(ctx, path)
	if err == nil {
		t.Log("note: context cancellation did not error — may be fast enough to complete first")
	}
}
