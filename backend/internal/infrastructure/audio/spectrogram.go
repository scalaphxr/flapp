package audio

import (
	"math"
	"os"
	"path/filepath"
	"strings"
)

// SpectrogramResult is a flat row-major time×frequency power map normalised
// to [0, 1]. Row i (frame i) starts at Data[i*Bins].
type SpectrogramResult struct {
	Data   []float64 `json:"data"`
	Frames int       `json:"frames"`
	Bins   int       `json:"bins"`
}

// ComputeSpectrogram decodes the audio at path and returns a time-frequency
// power map for spectrogram rendering. Only WAV/AIFF are supported; other
// formats return nil, errNoPCM so the caller can send an empty result.
//
// numFrames controls horizontal resolution (time columns) and numBins controls
// vertical resolution (log-spaced frequency bands).
// Audio is capped at 2 M samples to bound memory on long files.
func ComputeSpectrogram(path string, numFrames, numBins int) (*SpectrogramResult, error) {
	if numFrames < 1 {
		numFrames = 1
	}
	if numBins < 1 {
		numBins = 1
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	const maxSamples = 2_000_000
	var p *pcm
	switch ext {
	case "wav":
		p, err = decodeWAV(f, maxSamples)
	case "aiff", "aif":
		p, err = decodeAIFF(f, maxSamples)
	case "mp3":
		p, err = decodeMP3(f, maxSamples)
	default:
		return nil, errNoPCM
	}
	if err != nil || p == nil || len(p.samples) == 0 {
		return nil, errNoPCM
	}
	if p.sampleRate <= 0 {
		p.sampleRate = 44100
	}
	return buildSTFT(p, numFrames, numBins), nil
}

// buildSTFT computes a Short-Time Fourier Transform over the PCM data and
// returns a normalised time×frequency power map.
func buildSTFT(p *pcm, numFrames, numBins int) *SpectrogramResult {
	n := len(p.samples)

	// FFT size: 2048 gives ~21 Hz resolution at 44.1 kHz; halve for very
	// short sounds so we always have at least one full window.
	fftSize := 2048
	for fftSize > n && fftSize > 64 {
		fftSize >>= 1
	}
	fftSize = nextPow2(fftSize)
	if fftSize < 64 {
		fftSize = 64
	}

	w := hann(fftSize)
	hop := n / numFrames
	if hop < 1 {
		hop = 1
	}

	data := make([]float64, numFrames*numBins)
	re := make([]float64, fftSize)
	im := make([]float64, fftSize)

	for fi := 0; fi < numFrames; fi++ {
		start := fi * hop
		// Reset buffers.
		for i := range re {
			re[i] = 0
			im[i] = 0
		}
		end := start + fftSize
		if end > n {
			end = n
		}
		for i := 0; i < end-start; i++ {
			re[i] = p.samples[start+i] * w[i]
		}
		fft(re, im)
		// logBands returns log1p(sum_of_squared_magnitudes) per band — ideal
		// for perceptual-scale spectrograms.
		bands := logBands(re, im, fftSize, p.sampleRate, numBins)
		copy(data[fi*numBins:], bands)
	}

	// Normalise entire map to [0, 1].
	var maxV float64
	for _, v := range data {
		if v > maxV {
			maxV = v
		}
	}
	if maxV > 0 {
		inv := 1.0 / maxV
		for i := range data {
			data[i] = math.Min(1, data[i]*inv)
		}
	}

	return &SpectrogramResult{Data: data, Frames: numFrames, Bins: numBins}
}
