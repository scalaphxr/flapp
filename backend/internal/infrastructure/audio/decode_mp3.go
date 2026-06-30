package audio

import (
	"bufio"
	"io"

	mp3dec "github.com/hajimehoshi/go-mp3"
)

// decodeMP3 decodes an MP3 stream into mono float64 samples normalised to
// [-1, 1]. go-mp3 always outputs 16-bit little-endian stereo at the file's
// original sample rate, so we down-mix on the fly.
//
// maxSamples == 0 means "read everything".
func decodeMP3(r io.Reader, maxSamples int) (*pcm, error) {
	// Буферизуем чтение — go-mp3 читает поток небольшими кусками, bufio
	// снижает кол-во syscall при работе с os.File.
	br := bufio.NewReaderSize(r, 256<<10)
	dec, err := mp3dec.NewDecoder(br)
	if err != nil {
		return nil, errNoPCM
	}

	sampleRate := dec.SampleRate()

	// Предварительная аллокация: устраняет многократный reslice/copy при append.
	preallocSamples := maxSamples
	if preallocSamples <= 0 {
		preallocSamples = 48000 * 30 // пессимистичный потолок
	}
	out := &pcm{
		sampleRate: sampleRate,
		channels:   1,
		bitDepth:   16,
		samples:    make([]float64, 0, preallocSamples),
	}

	const stereoFrameBytes = 4 // 2 ch × 2 bytes per sample
	buf := make([]byte, 4096*stereoFrameBytes)

	for {
		n, err := dec.Read(buf)
		frames := n / stereoFrameBytes
		for i := 0; i < frames; i++ {
			l := int16(buf[i*4]) | int16(buf[i*4+1])<<8
			r := int16(buf[i*4+2]) | int16(buf[i*4+3])<<8
			mono := (float64(l) + float64(r)) / (2 * 32768.0)
			out.samples = append(out.samples, mono)
		}
		if maxSamples > 0 && len(out.samples) >= maxSamples {
			out.samples = out.samples[:maxSamples]
			break
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			// Some MP3 files have trailing garbage; stop on first unreadable frame.
			break
		}
	}

	if len(out.samples) == 0 {
		return nil, errNoPCM
	}
	return out, nil
}
