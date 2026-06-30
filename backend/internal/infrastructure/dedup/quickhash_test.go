package dedup_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/flapp/core/internal/infrastructure/dedup"
)

func TestQuickHash_SmallFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.wav")
	content := bytes.Repeat([]byte("hello world "), 100)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	h1 := dedup.QuickHash(path)
	h2 := dedup.QuickHash(path)
	if h1 == "" {
		t.Fatal("empty quick hash")
	}
	if h1 != h2 {
		t.Error("QuickHash is not deterministic")
	}
	if len(h1) < 4 || h1[:2] != "q:" {
		t.Errorf("expected q: prefix, got %q", h1)
	}
}

func TestQuickHash_DifferentContent(t *testing.T) {
	dir := t.TempDir()
	writeFile := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	p1 := writeFile("a.wav", "content one")
	p2 := writeFile("b.wav", "content two")

	if dedup.QuickHash(p1) == dedup.QuickHash(p2) {
		t.Error("different content should produce different quick hashes")
	}
}

func TestQuickHash_SameContent_DifferentName(t *testing.T) {
	dir := t.TempDir()
	content := bytes.Repeat([]byte("same"), 10000)
	p1 := filepath.Join(dir, "a.wav")
	p2 := filepath.Join(dir, "b.wav")
	if err := os.WriteFile(p1, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, content, 0o644); err != nil {
		t.Fatal(err)
	}

	if dedup.QuickHash(p1) != dedup.QuickHash(p2) {
		t.Error("identical content should produce identical quick hashes")
	}
}

func TestQuickHash_LargeFile(t *testing.T) {
	// Simulate a 200 KB file (larger than front+back block each).
	dir := t.TempDir()
	path := filepath.Join(dir, "large.wav")
	content := make([]byte, 200*1024)
	for i := range content {
		content[i] = byte(i)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	h := dedup.QuickHash(path)
	if h == "" {
		t.Fatal("empty hash for large file")
	}

	// Mutate last byte → hash must change.
	content[len(content)-1] ^= 0xFF
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	h2 := dedup.QuickHash(path)
	if h == h2 {
		t.Error("mutating last byte should change the quick hash")
	}
}

func TestQuickHashChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.wav")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := dedup.QuickHash(path)

	if dedup.QuickHashChanged(path, h) {
		t.Error("unchanged file should not appear changed")
	}
	if !dedup.QuickHashChanged(path, "q:deadbeef") {
		t.Error("wrong stored hash should appear changed")
	}
}

func TestQuickHash_MissingFile(t *testing.T) {
	h := dedup.QuickHash("/nonexistent/path/file.wav")
	if h != "" {
		t.Errorf("expected empty hash for missing file, got %q", h)
	}
}
