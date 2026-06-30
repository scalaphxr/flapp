package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// writeWAV writes 16-bit mono PCM to a .wav file for tests.
func writeWAV(t *testing.T, path string, sr int, samples []float64) {
	t.Helper()
	var buf bytes.Buffer
	dataLen := len(samples) * 2
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(36+dataLen))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))
	binary.Write(&buf, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(&buf, binary.LittleEndian, uint16(1)) // mono
	binary.Write(&buf, binary.LittleEndian, uint32(sr))
	binary.Write(&buf, binary.LittleEndian, uint32(sr*2))
	binary.Write(&buf, binary.LittleEndian, uint16(2))
	binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(dataLen))
	for _, s := range samples {
		if s > 1 {
			s = 1
		}
		if s < -1 {
			s = -1
		}
		binary.Write(&buf, binary.LittleEndian, int16(s*32767))
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write wav: %v", err)
	}
}

func sine(sr int, freq, secs float64) []float64 {
	n := int(float64(sr) * secs)
	out := make([]float64, n)
	for i := range out {
		out[i] = 0.8 * math.Sin(2*math.Pi*freq*float64(i)/float64(sr))
	}
	return out
}

func noise(sr int, secs float64, seed int64) []float64 {
	r := rand.New(rand.NewSource(seed))
	n := int(float64(sr) * secs)
	out := make([]float64, n)
	for i := range out {
		out[i] = (r.Float64()*2 - 1) * 0.8
	}
	return out
}

func TestAnalyzeFeatures(t *testing.T) {
	dir := t.TempDir()
	sr := 44100
	low := filepath.Join(dir, "sub808.wav")
	high := filepath.Join(dir, "hihat.wav")
	writeWAV(t, low, sr, sine(sr, 60, 0.5))   // deep sub
	writeWAV(t, high, sr, noise(sr, 0.1, 42)) // short bright noise burst

	a := NewAnalyzer()
	ctx := context.Background()

	lf, err := a.Analyze(ctx, low)
	if err != nil {
		t.Fatal(err)
	}
	if !lf.Analyzed || lf.SampleRate != sr {
		t.Fatalf("low: bad metadata %+v", lf)
	}
	if math.Abs(lf.DurationSeconds-0.5) > 0.02 {
		t.Fatalf("low: duration %.3f want ~0.5", lf.DurationSeconds)
	}
	if lf.LowEnergyRatio < 0.5 {
		t.Fatalf("low: expected most energy <150Hz, got %.2f", lf.LowEnergyRatio)
	}

	hf, err := a.Analyze(ctx, high)
	if err != nil {
		t.Fatal(err)
	}
	if hf.ZeroCrossRate <= lf.ZeroCrossRate {
		t.Fatalf("noise should have higher ZCR than sine: %.4f vs %.4f", hf.ZeroCrossRate, lf.ZeroCrossRate)
	}
	if hf.SpectralCentroid <= lf.SpectralCentroid {
		t.Fatalf("noise centroid (%.0f) should exceed sub centroid (%.0f)", hf.SpectralCentroid, lf.SpectralCentroid)
	}
}

func TestFingerprintRobustToVolume(t *testing.T) {
	dir := t.TempDir()
	sr := 44100
	// a mixed tonal signal so the spectrum has structure across time bins
	base := make([]float64, 0)
	base = append(base, sine(sr, 110, 0.3)...)
	base = append(base, sine(sr, 440, 0.3)...)
	base = append(base, noise(sr, 0.2, 7)...)

	orig := filepath.Join(dir, "orig.wav")
	quiet := filepath.Join(dir, "quiet.wav")
	other := filepath.Join(dir, "other.wav")

	writeWAV(t, orig, sr, base)
	half := make([]float64, len(base))
	for i := range base {
		half[i] = base[i] * 0.5 // same sound, quieter
	}
	writeWAV(t, quiet, sr, half)
	writeWAV(t, other, sr, noise(sr, 0.8, 99)) // unrelated

	a := NewAnalyzer()
	ctx := context.Background()
	fpOrig, _ := a.Fingerprint(ctx, orig)
	fpQuiet, _ := a.Fingerprint(ctx, quiet)
	fpOther, _ := a.Fingerprint(ctx, other)

	if fpOrig == "" {
		t.Fatal("empty fingerprint for wav")
	}
	dSame := HammingHex(fpOrig, fpQuiet)
	dDiff := HammingHex(fpOrig, fpOther)
	t.Logf("dist(same volume-changed)=%d  dist(different)=%d  bits=%d", dSame, dDiff, len(fpOrig)*4)
	if dSame < 0 || dDiff < 0 {
		t.Fatal("fingerprints not comparable")
	}
	if dSame >= dDiff {
		t.Fatalf("volume-changed copy (%d) should be closer than unrelated (%d)", dSame, dDiff)
	}
	// exact identity check
	if HammingHex(fpOrig, fpOrig) != 0 {
		t.Fatal("identical fingerprints must have distance 0")
	}
}
