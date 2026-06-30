package dedup

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/flapp/core/internal/infrastructure/audio"
)

func TestExactDuplicateDetection(t *testing.T) {
	ix := NewIndex(false, 0)
	ix.Add(1, "md5aaa", "sha111", "", "kick.wav")

	if id, kind, _ := ix.Check("md5aaa", "sha111", ""); kind != Exact || id != 1 {
		t.Errorf("exact by both = (%d,%s), want (1,exact)", id, kind)
	}
	if id, kind, _ := ix.Check("different", "sha111", ""); kind != Exact || id != 1 {
		t.Errorf("exact by sha = (%d,%s), want (1,exact)", id, kind)
	}
	if _, kind, _ := ix.Check("nope", "alsono", ""); kind != Unique {
		t.Errorf("unrelated = %s, want unique", kind)
	}
}

func TestAcousticDedupDisabledWithoutDeep(t *testing.T) {
	ix := NewIndex(false, 0)
	ix.Add(1, "", "", "ffff0000ffff0000", "a.wav")
	if _, kind, _ := ix.Check("", "", "ffff0000ffff0000"); kind != Unique {
		t.Errorf("with deep off, identical fp should NOT dedup; got %s", kind)
	}
}

func TestAcousticThresholdLogic(t *testing.T) {
	ix := NewIndex(true, 5)
	ix.Add(7, "", "", "00000000", "b.wav")

	// "0000000f" differs in 4 bits -> within threshold 5.
	if id, kind, _ := ix.Check("", "", "0000000f"); kind != Acoustic || id != 7 {
		t.Errorf("4-bit diff = (%d,%s), want (7,acoustic)", id, kind)
	}
	// "000000ff" differs in 8 bits -> beyond threshold 5.
	if _, kind, _ := ix.Check("", "", "000000ff"); kind != Unique {
		t.Errorf("8-bit diff should be unique, got %s", kind)
	}
}

func TestAccumulator(t *testing.T) {
	a := NewAccumulator()
	a.AddUnique(1000)
	a.AddUnique(2000)
	a.AddDuplicate(500)
	a.NoteProject()
	a.NoteArchive()

	s := a.Stats()
	if s.FilesFound != 3 || s.UniqueFiles != 2 || s.Duplicates != 1 {
		t.Errorf("counts = %+v", s)
	}
	if s.BytesTotal != 3500 || s.BytesSaved != 500 {
		t.Errorf("bytes = total %d saved %d", s.BytesTotal, s.BytesSaved)
	}
	if s.ProjectsParsed != 1 || s.ArchivesOpened != 1 {
		t.Errorf("project/archive = %d/%d", s.ProjectsParsed, s.ArchivesOpened)
	}
}

// writeSineWAV writes a mono 16-bit PCM WAV of a sine wave.
func writeSineWAV(t *testing.T, path string, freq, amp, secs float64, sr int) {
	t.Helper()
	n := int(float64(sr) * secs)
	var buf []byte
	hdr := make([]byte, 44)
	copy(hdr[0:4], "RIFF")
	dataSize := n * 2
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(36+dataSize))
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:20], 16)
	binary.LittleEndian.PutUint16(hdr[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(hdr[22:24], 1) // mono
	binary.LittleEndian.PutUint32(hdr[24:28], uint32(sr))
	binary.LittleEndian.PutUint32(hdr[28:32], uint32(sr*2))
	binary.LittleEndian.PutUint16(hdr[32:34], 2)
	binary.LittleEndian.PutUint16(hdr[34:36], 16)
	copy(hdr[36:40], "data")
	binary.LittleEndian.PutUint32(hdr[40:44], uint32(dataSize))
	buf = append(buf, hdr...)

	for i := 0; i < n; i++ {
		v := amp * math.Sin(2*math.Pi*freq*float64(i)/float64(sr))
		s := int16(v * 32767)
		buf = append(buf, byte(s), byte(s>>8))
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatalf("write wav: %v", err)
	}
}

// TestAcousticDedupRealAudio verifies the end-to-end behaviour: the same sound
// at a different volume collapses, but a clearly different sound does not.
func TestAcousticDedupRealAudio(t *testing.T) {
	dir := t.TempDir()
	loud := filepath.Join(dir, "loud.wav")
	quiet := filepath.Join(dir, "quiet.wav")
	other := filepath.Join(dir, "other.wav")

	writeSineWAV(t, loud, 220, 0.9, 1.0, 44100)
	writeSineWAV(t, quiet, 220, 0.35, 1.0, 44100) // same tone, much quieter
	writeSineWAV(t, other, 3500, 0.9, 1.0, 44100) // very different tone

	an := audio.NewAnalyzer()
	ctx := context.Background()
	fpLoud, err := an.Fingerprint(ctx, loud)
	if err != nil {
		t.Fatalf("fingerprint loud: %v", err)
	}
	fpQuiet, _ := an.Fingerprint(ctx, quiet)
	fpOther, _ := an.Fingerprint(ctx, other)

	ix := NewIndex(true, 0) // default threshold
	ix.Add(1, "h1", "s1", fpLoud, "c.wav")

	if id, kind, _ := ix.Check("h2", "s2", fpQuiet); kind != Acoustic || id != 1 {
		t.Errorf("same tone quieter should be acoustic dup of 1; got (%d,%s), dist=%d",
			id, kind, audio.HammingHex(fpLoud, fpQuiet))
	}
	if _, kind, _ := ix.Check("h3", "s3", fpOther); kind != Unique {
		t.Errorf("different tone should be unique; got %s, dist=%d",
			kind, audio.HammingHex(fpLoud, fpOther))
	}
}
