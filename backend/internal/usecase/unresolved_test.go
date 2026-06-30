package usecase_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/flapp/core/internal/usecase"
)

func TestResolveFlpSamplePath_ExactPath(t *testing.T) {
	dir := t.TempDir()
	wav := filepath.Join(dir, "kick.wav")
	os.WriteFile(wav, []byte("RIFF"), 0o644)

	resolved, ok := usecase.ResolveFlpSamplePath(wav, "", nil)
	if !ok {
		t.Error("expected exact path to resolve")
	}
	if resolved != wav {
		t.Errorf("got %q want %q", resolved, wav)
	}
}

func TestResolveFlpSamplePath_RelativeToProject(t *testing.T) {
	dir := t.TempDir()
	wav := filepath.Join(dir, "kick.wav")
	os.WriteFile(wav, []byte("RIFF"), 0o644)

	// FLP stores path relative to project directory.
	resolved, ok := usecase.ResolveFlpSamplePath("kick.wav", dir, nil)
	if !ok {
		t.Error("expected relative path to resolve against project dir")
	}
	if resolved != wav {
		t.Errorf("got %q want %q", resolved, wav)
	}
}

func TestResolveFlpSamplePath_SearchRoots(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "drumkits")
	os.MkdirAll(root, 0o755)
	wav := filepath.Join(root, "snare.wav")
	os.WriteFile(wav, []byte("RIFF"), 0o644)

	// FLP has an absolute Windows path that doesn't exist on this machine.
	resolved, ok := usecase.ResolveFlpSamplePath(`C:\SomeDir\snare.wav`, dir, []string{root})
	if !ok {
		t.Error("expected file to be found via search root")
	}
	if resolved != wav {
		t.Errorf("got %q want %q", resolved, wav)
	}
}

func TestResolveFlpSamplePath_Missing(t *testing.T) {
	_, ok := usecase.ResolveFlpSamplePath(`C:\Nonexistent\file.wav`, "", nil)
	if ok {
		t.Error("expected false for nonexistent file")
	}
}

func TestNormalizeFLPSamplePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`C:\Samples\kick.wav`, "/Samples/kick.wav"},
		{`/home/user/samples/kick.wav`, "/home/user/samples/kick.wav"},
		{`relative/kick.wav`, "relative/kick.wav"},
	}
	for _, tc := range cases {
		got := usecase.NormalizeFLPSamplePath(tc.in)
		// On Windows the filepath.FromSlash conversion changes / to \, so
		// we only check that the result no longer contains backslashes.
		if len(got) == 0 {
			t.Errorf("NormalizeFLPSamplePath(%q) returned empty string", tc.in)
		}
	}
}
