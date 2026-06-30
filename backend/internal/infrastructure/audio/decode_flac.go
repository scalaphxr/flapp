package audio

import (
	"io"

	"github.com/mewkiz/flac"
)

// decodeFLAC декодирует FLAC-поток в моно float64 [-1, 1].
// Использует чисто-Go декодер mewkiz/flac без CGO.
// Стерео и многоканальное аудио усредняется в моно.
func decodeFLAC(r io.Reader, maxSamples int) (*pcm, error) {
	stream, err := flac.New(r)
	if err != nil {
		return nil, errNoPCM
	}
	defer stream.Close()

	channels := int(stream.Info.NChannels)
	sampleRate := int(stream.Info.SampleRate)
	bits := int(stream.Info.BitsPerSample)

	if channels <= 0 {
		channels = 1
	}
	if sampleRate <= 0 {
		sampleRate = 44100
	}
	// Масштаб для нормализации int32 → float64: 2^(bits-1).
	var scale float64
	if bits > 0 && bits <= 32 {
		scale = float64(int64(1) << uint(bits-1))
	}
	if scale == 0 {
		scale = 32768
	}

	preallocSamples := maxSamples
	if preallocSamples <= 0 {
		preallocSamples = sampleRate * 30
	}
	out := &pcm{
		sampleRate: sampleRate,
		channels:   channels,
		bitDepth:   bits,
		samples:    make([]float64, 0, preallocSamples),
	}

	for {
		frame, err := stream.ParseNext()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Некоторые FLAC-файлы содержат мусор в конце; продолжаем с тем, что есть.
			break
		}

		nSamples := len(frame.Subframes[0].Samples)
		nCh := len(frame.Subframes)
		for i := 0; i < nSamples; i++ {
			var acc float64
			for c := 0; c < nCh; c++ {
				acc += float64(frame.Subframes[c].Samples[i]) / scale
			}
			out.samples = append(out.samples, acc/float64(nCh))
		}
		if maxSamples > 0 && len(out.samples) >= maxSamples {
			out.samples = out.samples[:maxSamples]
			break
		}
	}

	if len(out.samples) == 0 {
		return nil, errNoPCM
	}
	return out, nil
}
