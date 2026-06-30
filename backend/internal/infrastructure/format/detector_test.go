package format_test

import (
	"testing"

	"github.com/flapp/core/internal/infrastructure/format"
)

func TestDetectBytes(t *testing.T) {
	cases := []struct {
		name string
		hdr  []byte
		want format.Format
	}{
		{"ZIP local", []byte{'P', 'K', 0x03, 0x04, 0, 0, 0, 0, 0, 0, 0, 0}, format.ZIP},
		{"ZIP empty", []byte{'P', 'K', 0x05, 0x06, 0, 0, 0, 0, 0, 0, 0, 0}, format.ZIP},
		{"7z", []byte{'7', 'z', 0xBC, 0xAF, 0x27, 0x1C}, format.SevenZ},
		{"RAR v4", []byte{'R', 'a', 'r', '!', 0x1A, 0x07, 0x00}, format.RAR},
		{"RAR v5", []byte{'R', 'a', 'r', '!', 0x1A, 0x07, 0x01}, format.RAR},
		{"FLAC", []byte{'f', 'L', 'a', 'C', 0, 0, 0, 0}, format.FLAC},
		{"OGG", []byte{'O', 'g', 'g', 'S', 0, 0, 0, 0}, format.OGG},
		{"MIDI", []byte{'M', 'T', 'h', 'd', 0, 0, 0, 6}, format.MIDI},
		{"FLP", []byte{'F', 'L', 'h', 'd', 0, 0, 0, 0}, format.FLP},
		{"WAV", []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'A', 'V', 'E'}, format.WAV},
		{"AIFF RIFF", []byte{'F', 'O', 'R', 'M', 0, 0, 0, 0, 'A', 'I', 'F', 'F'}, format.AIFF},
		{"ID3/MP3", []byte{'I', 'D', '3', 0x03, 0x00, 0x00}, format.MP3},
		{"MP3 sync", []byte{0xFF, 0xFB, 0x90, 0x00}, format.MP3},
		{"M4A ftyp", []byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '}, format.M4A},
		{"too short", []byte{0x00}, format.Unknown},
		{"random", []byte{0xDE, 0xAD, 0xBE, 0xEF}, format.Unknown},
	}
	for _, tc := range cases {
		got := format.DetectBytes(tc.hdr)
		if got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestFormatPredicates(t *testing.T) {
	if !format.WAV.IsAudio() {
		t.Error("WAV should be audio")
	}
	if !format.ZIP.IsArchive() {
		t.Error("ZIP should be archive")
	}
	if !format.SevenZ.IsArchive() {
		t.Error("7z should be archive")
	}
	if !format.RAR.IsArchive() {
		t.Error("RAR should be archive")
	}
	if !format.FLP.IsFLP() {
		t.Error("FLP should be FLP")
	}
	if !format.MIDI.IsMIDI() {
		t.Error("MIDI should be MIDI")
	}
	if format.Unknown.IsAudio() || format.Unknown.IsArchive() {
		t.Error("Unknown should not match anything")
	}
}

func TestFormatFromExt(t *testing.T) {
	cases := []struct {
		ext  string
		want format.Format
	}{
		{".wav", format.WAV},
		{"wav", format.WAV},
		{".WAV", format.WAV},
		{".mp3", format.MP3},
		{".flac", format.FLAC},
		{".ogg", format.OGG},
		{".aiff", format.AIFF},
		{".aif", format.AIFF},
		{".m4a", format.M4A},
		{".mid", format.MIDI},
		{".midi", format.MIDI},
		{".flp", format.FLP},
		{".zip", format.ZIP},
		{".7z", format.SevenZ},
		{".rar", format.RAR},
		{".xyz", format.Unknown},
	}
	for _, tc := range cases {
		got := format.FormatFromExt(tc.ext)
		if got != tc.want {
			t.Errorf("FormatFromExt(%q) = %q, want %q", tc.ext, got, tc.want)
		}
	}
}

func TestPathNormalization(t *testing.T) {
	// Verify that non-standard extensions are not auto-assigned audio formats.
	for _, bad := range []string{".exe", ".dll", ".txt", ".docx"} {
		if format.FormatFromExt(bad).IsAudio() {
			t.Errorf("FormatFromExt(%q) should not be audio", bad)
		}
	}
}
