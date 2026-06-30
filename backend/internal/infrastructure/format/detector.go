// Package format detects file types by content signature (magic bytes), not
// by file extension. This is Stage A of the analysis pipeline: a file can be
// mislabeled, extension-stripped, or embedded in an archive with a wrong name.
package format

import (
	"os"
	"strings"
)

// Format names a detected file type.
type Format string

const (
	WAV     Format = "wav"
	AIFF    Format = "aiff"
	FLAC    Format = "flac"
	MP3     Format = "mp3"
	OGG     Format = "ogg"
	M4A     Format = "m4a"
	MIDI    Format = "mid"
	FLP     Format = "flp"
	ZIP     Format = "zip"
	SevenZ  Format = "7z"
	RAR     Format = "rar"
	Unknown Format = ""
)

// DetectFile reads up to 16 bytes from the file at path.
// Returns Unknown on any error.
func DetectFile(path string) Format {
	f, err := os.Open(path)
	if err != nil {
		return Unknown
	}
	defer f.Close()
	var buf [16]byte
	n, _ := f.Read(buf[:])
	return DetectBytes(buf[:n])
}

// DetectBytes identifies the format from the leading bytes of a file.
// Requires at least 4 bytes for most formats; 12 for WAV/AIFF.
func DetectBytes(hdr []byte) Format {
	if len(hdr) < 2 {
		return Unknown
	}
	switch {
	// ZIP: local file header PK\x03\x04, or empty PK\x05\x06
	case len(hdr) >= 4 && hdr[0] == 'P' && hdr[1] == 'K' &&
		(hdr[2] == 0x03 && hdr[3] == 0x04 || hdr[2] == 0x05 && hdr[3] == 0x06):
		return ZIP

	// 7z: 7z\xBC\xAF\x27\x1C
	case len(hdr) >= 6 && hdr[0] == '7' && hdr[1] == 'z' &&
		hdr[2] == 0xBC && hdr[3] == 0xAF && hdr[4] == 0x27 && hdr[5] == 0x1C:
		return SevenZ

	// RAR v4: Rar!\x1A\x07\x00  RAR v5: Rar!\x1A\x07\x01
	case len(hdr) >= 7 && hdr[0] == 'R' && hdr[1] == 'a' && hdr[2] == 'r' &&
		hdr[3] == '!' && hdr[4] == 0x1A && hdr[5] == 0x07:
		return RAR

	// FLAC: fLaC
	case len(hdr) >= 4 && hdr[0] == 'f' && hdr[1] == 'L' && hdr[2] == 'a' && hdr[3] == 'C':
		return FLAC

	// OGG: OggS
	case len(hdr) >= 4 && hdr[0] == 'O' && hdr[1] == 'g' && hdr[2] == 'g' && hdr[3] == 'S':
		return OGG

	// MIDI: MThd
	case len(hdr) >= 4 && hdr[0] == 'M' && hdr[1] == 'T' && hdr[2] == 'h' && hdr[3] == 'd':
		return MIDI

	// FLP: FLhd
	case len(hdr) >= 4 && hdr[0] == 'F' && hdr[1] == 'L' && hdr[2] == 'h' && hdr[3] == 'd':
		return FLP

	// RIFF container — check sub-type at offset 8
	case len(hdr) >= 12 && hdr[0] == 'R' && hdr[1] == 'I' && hdr[2] == 'F' && hdr[3] == 'F':
		sub := string(hdr[8:12])
		switch sub {
		case "WAVE":
			return WAV
		case "AIFF", "AIFC":
			return AIFF
		}
		return Unknown

	// AIFF: FORM...AIFF or FORM...AIFC
	case len(hdr) >= 12 && hdr[0] == 'F' && hdr[1] == 'O' && hdr[2] == 'R' && hdr[3] == 'M':
		if len(hdr) >= 12 {
			sub := string(hdr[8:12])
			if sub == "AIFF" || sub == "AIFC" {
				return AIFF
			}
		}
		return Unknown

	// ID3 tag → MP3
	case len(hdr) >= 3 && hdr[0] == 'I' && hdr[1] == 'D' && hdr[2] == '3':
		return MP3

	// MP3 frame sync: 0xFF followed by 0xE0–0xFF (MPEG sync word, any layer/bitrate)
	case len(hdr) >= 2 && hdr[0] == 0xFF && (hdr[1]&0xE0) == 0xE0:
		return MP3

	// MP4/M4A: 4-byte box size then "ftyp" at offset 4
	case len(hdr) >= 8 && hdr[4] == 'f' && hdr[5] == 't' && hdr[6] == 'y' && hdr[7] == 'p':
		return M4A
	}
	return Unknown
}

// IsAudio reports whether f is a supported audio format.
func (f Format) IsAudio() bool {
	switch f {
	case WAV, AIFF, FLAC, MP3, OGG, M4A:
		return true
	}
	return false
}

// IsArchive reports whether f is a supported archive container.
func (f Format) IsArchive() bool {
	switch f {
	case ZIP, SevenZ, RAR:
		return true
	}
	return false
}

// IsMIDI reports whether f is Standard MIDI.
func (f Format) IsMIDI() bool { return f == MIDI }

// IsFLP reports whether f is an FL Studio project.
func (f Format) IsFLP() bool { return f == FLP }

// Extension returns the canonical extension (without dot) for the format.
func (f Format) Extension() string { return string(f) }

// FormatFromExt maps a lowercase file extension (with or without leading dot)
// to the best-guess Format. Used as a fast pre-filter before magic detection.
func FormatFromExt(ext string) Format {
	ext = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
	switch ext {
	case "wav":
		return WAV
	case "aiff", "aif":
		return AIFF
	case "flac":
		return FLAC
	case "mp3":
		return MP3
	case "ogg":
		return OGG
	case "m4a":
		return M4A
	case "mid", "midi":
		return MIDI
	case "flp":
		return FLP
	case "zip":
		return ZIP
	case "7z":
		return SevenZ
	case "rar":
		return RAR
	}
	return Unknown
}
