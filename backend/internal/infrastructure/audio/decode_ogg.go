package audio

import (
	"io"

	"github.com/jfreymuth/oggvorbis"
)

// decodeOGG декодирует OGG/Vorbis-поток в моно float64 [-1, 1].
// Использует чисто-Go декодер jfreymuth/oggvorbis без CGO.
// Read возвращает interleaved-сэмплы всех каналов; мы усредняем в моно.
func decodeOGG(r io.Reader, maxSamples int) (*pcm, error) {
	vr, err := oggvorbis.NewReader(r)
	if err != nil {
		return nil, errNoPCM
	}

	channels := vr.Channels()
	sampleRate := vr.SampleRate()

	if channels <= 0 {
		channels = 1
	}
	if sampleRate <= 0 {
		sampleRate = 44100
	}

	preallocSamples := maxSamples
	if preallocSamples <= 0 {
		preallocSamples = sampleRate * 30
	}
	out := &pcm{
		sampleRate: sampleRate,
		channels:   channels,
		bitDepth:   32,
		samples:    make([]float64, 0, preallocSamples),
	}

	// Read возвращает interleaved float32: [L0, R0, L1, R1, ...].
	// n — количество float32-значений (фреймов × каналов).
	buf := make([]float32, 4096*channels)
	for {
		n, err := vr.Read(buf)
		frames := n / channels
		for i := 0; i < frames; i++ {
			var acc float64
			for c := 0; c < channels; c++ {
				acc += float64(buf[i*channels+c])
			}
			out.samples = append(out.samples, acc/float64(channels))
		}
		if maxSamples > 0 && len(out.samples) >= maxSamples {
			out.samples = out.samples[:maxSamples]
			break
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
	}

	if len(out.samples) == 0 {
		return nil, errNoPCM
	}
	return out, nil
}
