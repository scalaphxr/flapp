package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
)

// probeCompressed extracts sample rate, channels and duration from compressed
// containers (mp3, flac, ogg, m4a/mp4) by reading headers only — no full decode.
func probeCompressed(path, ext string) (sampleRate, channels int, durationSec float64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, 0, err
	}
	defer f.Close()
	switch ext {
	case "flac":
		return probeFLAC(f)
	case "mp3":
		return probeMP3(f)
	case "ogg":
		return probeOGG(f)
	case "m4a", "mp4", "aac":
		return probeMP4(f)
	}
	return 0, 0, 0, errors.New("unsupported container")
}

func probeFLAC(f *os.File) (int, int, float64, error) {
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return 0, 0, 0, err
	}
	if string(magic[:]) != "fLaC" {
		return 0, 0, 0, errors.New("not flac")
	}
	// first metadata block must be STREAMINFO
	var head [4]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return 0, 0, 0, err
	}
	body := make([]byte, 34)
	if _, err := io.ReadFull(f, body); err != nil {
		return 0, 0, 0, err
	}
	// bytes 10..17 pack: sampleRate(20) channels(3) bitsPerSample(5) totalSamples(36)
	sr := (int(body[10]) << 12) | (int(body[11]) << 4) | (int(body[12]) >> 4)
	ch := int((body[12]>>1)&0x07) + 1
	totalSamples := (int64(body[13]&0x0f) << 32) | (int64(body[14]) << 24) |
		(int64(body[15]) << 16) | (int64(body[16]) << 8) | int64(body[17])
	dur := 0.0
	if sr > 0 {
		dur = float64(totalSamples) / float64(sr)
	}
	return sr, ch, dur, nil
}

var mp3BitrateV1L3 = []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
var mp3SampleRateV1 = []int{44100, 48000, 32000, 0}
var mp3SampleRateV2 = []int{22050, 24000, 16000, 0}

func probeMP3(f *os.File) (int, int, float64, error) {
	info, _ := f.Stat()
	size := info.Size()

	// skip ID3v2 tag if present
	var id3 [10]byte
	start := int64(0)
	if _, err := io.ReadFull(f, id3[:]); err == nil && string(id3[0:3]) == "ID3" {
		tagSize := (int64(id3[6]&0x7f) << 21) | (int64(id3[7]&0x7f) << 14) |
			(int64(id3[8]&0x7f) << 7) | int64(id3[9]&0x7f)
		start = 10 + tagSize
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return 0, 0, 0, err
	}

	// find the first frame sync
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	idx := -1
	for i := 0; i+1 < n; i++ {
		if buf[i] == 0xFF && buf[i+1]&0xE0 == 0xE0 {
			idx = i
			break
		}
	}
	if idx < 0 || idx+4 > n {
		return 0, 0, 0, errors.New("no mp3 frame sync")
	}
	h := buf[idx : idx+4]
	versionBits := (h[1] >> 3) & 0x03 // 3 = MPEG1, 2 = MPEG2
	bitrateIdx := (h[2] >> 4) & 0x0F
	srIdx := (h[2] >> 2) & 0x03
	channelMode := (h[3] >> 6) & 0x03

	var sr int
	if versionBits == 3 {
		sr = mp3SampleRateV1[srIdx]
	} else {
		sr = mp3SampleRateV2[srIdx]
	}
	br := mp3BitrateV1L3[bitrateIdx] * 1000
	ch := 2
	if channelMode == 3 {
		ch = 1
	}
	dur := 0.0
	if br > 0 {
		dur = float64((size-start)*8) / float64(br)
	}
	return sr, ch, dur, nil
}

func probeOGG(f *os.File) (int, int, float64, error) {
	// read first page to get the vorbis/opus identification header
	head := make([]byte, 4096)
	n, _ := f.Read(head)
	sr := 0
	ch := 0
	if i := bytes.Index(head[:n], []byte("\x01vorbis")); i >= 0 && i+29 <= n {
		ch = int(head[i+11])
		sr = int(binary.LittleEndian.Uint32(head[i+12 : i+16]))
	} else if i := bytes.Index(head[:n], []byte("OpusHead")); i >= 0 && i+19 <= n {
		ch = int(head[i+9])
		sr = int(binary.LittleEndian.Uint32(head[i+12 : i+16]))
	}
	if sr == 0 {
		return 0, 0, 0, errors.New("no ogg id header")
	}

	// last granule position lives in the final OggS page
	info, _ := f.Stat()
	size := info.Size()
	tailLen := int64(65536)
	if size < tailLen {
		tailLen = size
	}
	tail := make([]byte, tailLen)
	if _, err := f.Seek(size-tailLen, io.SeekStart); err == nil {
		io.ReadFull(f, tail)
	}
	last := bytes.LastIndex(tail, []byte("OggS"))
	dur := 0.0
	if last >= 0 && last+14 <= len(tail) {
		granule := binary.LittleEndian.Uint64(tail[last+6 : last+14])
		dur = float64(granule) / float64(sr)
	}
	return sr, ch, dur, nil
}

func probeMP4(f *os.File) (int, int, float64, error) {
	// walk top-level atoms looking for moov, then mvhd inside it
	moovOff, moovSize, err := findAtom(f, 0, mustStat(f), "moov")
	if err != nil {
		return 0, 0, 0, err
	}
	mvhdOff, _, err := findAtom(f, moovOff+8, moovOff+moovSize, "mvhd")
	if err != nil {
		return 0, 0, 0, err
	}
	box := make([]byte, 32)
	if _, err := f.ReadAt(box, mvhdOff+8); err != nil {
		return 0, 0, 0, err
	}
	version := box[0]
	var timescale, duration uint64
	if version == 1 {
		timescale = uint64(binary.BigEndian.Uint32(box[20:24]))
		duration = binary.BigEndian.Uint64(box[24:32])
	} else {
		timescale = uint64(binary.BigEndian.Uint32(box[12:16]))
		duration = uint64(binary.BigEndian.Uint32(box[16:20]))
	}
	dur := 0.0
	if timescale > 0 {
		dur = float64(duration) / float64(timescale)
	}
	// channels/sample-rate would require descending into stsd; duration is enough
	return 0, 0, dur, nil
}

func mustStat(f *os.File) int64 {
	if fi, err := f.Stat(); err == nil {
		return fi.Size()
	}
	return 0
}

// findAtom scans MP4 boxes in [from,to) and returns the offset+size of name.
func findAtom(f *os.File, from, to int64, name string) (int64, int64, error) {
	pos := from
	hdr := make([]byte, 8)
	for pos+8 <= to {
		if _, err := f.ReadAt(hdr, pos); err != nil {
			return 0, 0, err
		}
		size := int64(binary.BigEndian.Uint32(hdr[0:4]))
		atom := string(hdr[4:8])
		if size == 1 {
			// 64-bit size
			ext := make([]byte, 8)
			if _, err := f.ReadAt(ext, pos+8); err != nil {
				return 0, 0, err
			}
			size = int64(binary.BigEndian.Uint64(ext))
		}
		if size < 8 {
			break
		}
		if atom == name {
			return pos, size, nil
		}
		pos += size
	}
	return 0, 0, errors.New("atom not found: " + name)
}
