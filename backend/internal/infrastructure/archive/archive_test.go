package archive

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/flapp/core/internal/domain"
)

// makeZip creates a zip at path containing the given name->content entries.
func makeZip(t *testing.T, zipPath string, files map[string]string) {
	t.Helper()
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestZipExtractWithFilter(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "kit.zip")
	makeZip(t, zipPath, map[string]string{
		"Drums/kick.wav":    "KICKDATA",
		"Drums/snare.wav":   "SNAREDATA",
		"readme.txt":        "ignore me",
		"art/cover.png":     "PNGDATA",
		"Melodies/lead.mp3": "MP3DATA",
	})

	ex := New([]string{".wav", ".mp3"})
	if !ex.Supports(".zip") || !ex.Supports("ZIP") {
		t.Fatal("zip should be supported")
	}
	if ex.Supports(".tar") {
		t.Fatal("tar should not be supported")
	}

	destDir := filepath.Join(dir, "out")
	var got []domain.ExtractedFile
	err := ex.Extract(context.Background(), zipPath, destDir, func(e domain.ExtractedFile) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("extracted %d files, want 3 (wav+mp3 only): %+v", len(got), got)
	}
	names := []string{}
	for _, e := range got {
		names = append(names, e.Name)
		data, err := os.ReadFile(e.TempPath)
		if err != nil {
			t.Errorf("temp file missing: %v", err)
		}
		if len(data) == 0 {
			t.Errorf("temp file %s empty", e.Name)
		}
	}
	sort.Strings(names)
	want := []string{"kick.wav", "lead.mp3", "snare.wav"}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("names = %v, want %v", names, want)
			break
		}
	}

	// RelPath must keep the archive-internal folder for classification hints.
	for _, e := range got {
		if e.Name == "kick.wav" && e.RelPath != "Drums/kick.wav" {
			t.Errorf("relpath = %q, want Drums/kick.wav", e.RelPath)
		}
	}
}

func TestZipSlipDefeated(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "evil.zip")
	makeZip(t, zipPath, map[string]string{
		"../../escape.wav": "EVIL",
	})

	ex := New(nil)
	destDir := filepath.Join(dir, "out")
	var paths []string
	err := ex.Extract(context.Background(), zipPath, destDir, func(e domain.ExtractedFile) error {
		paths = append(paths, e.TempPath)
		return nil
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, p := range paths {
		abs, _ := filepath.Abs(p)
		absDest, _ := filepath.Abs(destDir)
		rel, err := filepath.Rel(absDest, abs)
		if err != nil || filepath.IsAbs(rel) || rel == ".." || filepath.Dir(rel) != "." {
			t.Errorf("file escaped destDir: %s (rel %s)", p, rel)
		}
	}
}

func TestPackerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	srcA := filepath.Join(dir, "a.wav")
	srcB := filepath.Join(dir, "b.wav")
	os.WriteFile(srcA, []byte("AAA"), 0o644)
	os.WriteFile(srcB, []byte("BBB"), 0o644)

	dest := filepath.Join(dir, "pack.zip")
	p := NewPacker()
	entries := []domain.PackEntry{
		{SourcePath: srcA, ArcPath: "808/a.wav"},
		{SourcePath: srcB, ArcPath: "808/a.wav"},                          // duplicate arc path -> must de-collide
		{SourcePath: filepath.Join(dir, "missing.wav"), ArcPath: "x.wav"}, // skipped
	}
	var lastDone, lastTotal int
	if err := p.Pack(context.Background(), dest, "zip", entries, func(done, total int) {
		lastDone, lastTotal = done, total
	}); err != nil {
		t.Fatalf("pack: %v", err)
	}
	if lastTotal != 3 || lastDone == 0 {
		t.Errorf("progress final = %d/%d", lastDone, lastTotal)
	}

	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatalf("open pack: %v", err)
	}
	defer zr.Close()
	if len(zr.File) != 2 {
		t.Fatalf("pack has %d files, want 2", len(zr.File))
	}
	seen := map[string]bool{}
	for _, f := range zr.File {
		seen[f.Name] = true
	}
	if !seen["808/a.wav"] || !seen["808/a (1).wav"] {
		t.Errorf("expected de-collided names, got %v", seen)
	}
}

func TestPackerRejectsNonZip(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "pack.7z")
	err := NewPacker().Pack(context.Background(), dest, "7z", nil, nil)
	if err == nil {
		t.Fatal("expected error for non-zip format")
	}
}
