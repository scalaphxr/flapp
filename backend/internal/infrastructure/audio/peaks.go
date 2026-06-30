package audio

import (
	"math"
	"os"
	"path/filepath"
	"strings"
)

// PeakMinMax декодирует аудио из path и возвращает numBins пар [min, max]
// амплитуд, нормализованных в [-1, 1]. Каждая пара соответствует одному
// бакету PCM-данных: [0] — минимум (отрицательный пик), [1] — максимум
// (положительный пик). Это даёт заполненную симметричную форму как в FL Studio.
//
// Поддерживаются: WAV, AIFF, MP3, FLAC, OGG/Vorbis.
// Для M4A/AAC и других неподдерживаемых форматов возвращается errNoPCM.
//
// Декодирование ограничено 2 000 000 сэмплов (~45 с при 44.1 кГц), чтобы
// удержать потребление памяти предсказуемым для длинных файлов.
func PeakMinMax(path string, numBins int) ([][2]float64, error) {
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
	case "flac":
		p, err = decodeFLAC(f, maxSamples)
	case "ogg", "oga":
		p, err = decodeOGG(f, maxSamples)
	default:
		return nil, errNoPCM
	}
	if err != nil || p == nil || len(p.samples) == 0 {
		return nil, errNoPCM
	}
	return peakMinMaxBins(p.samples, numBins), nil
}

// peakMinMaxBins делит samples на numBins равных окон и возвращает пару
// [min, max] для каждого окна. Значения нормализованы в [-1, 1].
// Результат позволяет нарисовать заполненную волну: для каждой точки x
// рисуется вертикальная линия от min до max.
func peakMinMaxBins(samples []float64, numBins int) [][2]float64 {
	n := len(samples)
	out := make([][2]float64, numBins)
	for i := range out {
		start := i * n / numBins
		end := (i + 1) * n / numBins
		if end > n {
			end = n
		}
		if start >= end {
			// Пустой бакет — нейтральное значение.
			out[i] = [2]float64{0, 0}
			continue
		}
		lo, hi := samples[start], samples[start]
		for _, s := range samples[start+1 : end] {
			if s < lo {
				lo = s
			}
			if s > hi {
				hi = s
			}
		}
		// Clamp в [-1, 1] на случай битых файлов с выходящими за диапазон данными.
		out[i] = [2]float64{math.Max(-1, lo), math.Min(1, hi)}
	}
	return out
}
