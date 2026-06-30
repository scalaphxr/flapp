package audio

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// Вспомогательные функции writeWAV, sine, noise определены в analyzer_test.go
// и доступны здесь как часть того же пакета audio.

func TestPeaksLength(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sine.wav")
	writeWAV(t, path, 44100, sine(44100, 440, 1.0))

	for _, bins := range []int{1, 10, 80, 400, 1500} {
		p, err := PeakMinMax(path, bins)
		if err != nil {
			t.Fatalf("bins=%d: неожиданная ошибка: %v", bins, err)
		}
		if len(p) != bins {
			t.Fatalf("bins=%d: ожидали %d пар, получили %d", bins, bins, len(p))
		}
	}
}

func TestPeaksNormalization(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "noise.wav")
	writeWAV(t, path, 44100, noise(44100, 0.5, 42))

	p, err := PeakMinMax(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range p {
		if v[0] < -1 || v[0] > 1 {
			t.Fatalf("бин %d: min=%.4f вне диапазона [-1,1]", i, v[0])
		}
		if v[1] < -1 || v[1] > 1 {
			t.Fatalf("бин %d: max=%.4f вне диапазона [-1,1]", i, v[1])
		}
		if v[0] > v[1] {
			t.Fatalf("бин %d: min %.4f > max %.4f", i, v[0], v[1])
		}
	}
}

func TestPeaksMaxAmplitude(t *testing.T) {
	// Тишина с одним положительным пиком амплитуды 0.9 ровно в середине.
	samples := make([]float64, 44100)
	samples[22050] = 0.9

	dir := t.TempDir()
	path := filepath.Join(dir, "spike.wav")
	writeWAV(t, path, 44100, samples)

	p, err := PeakMinMax(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	var maxHi float64
	for _, v := range p {
		if v[1] > maxHi {
			maxHi = v[1]
		}
	}
	// Допуск ±0.01 из-за квантования 16-bit PCM.
	if math.Abs(maxHi-0.9) > 0.01 {
		t.Fatalf("ожидали максимальный hi-пик ~0.9, получили %.4f", maxHi)
	}
}

func TestPeaksMinAmplitude(t *testing.T) {
	// Тишина с одним отрицательным пиком -0.9 — проверяем min-канал пары.
	samples := make([]float64, 44100)
	samples[22050] = -0.9

	dir := t.TempDir()
	path := filepath.Join(dir, "neg_spike.wav")
	writeWAV(t, path, 44100, samples)

	p, err := PeakMinMax(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	var minLo float64
	for _, v := range p {
		if v[0] < minLo {
			minLo = v[0]
		}
	}
	if math.Abs(minLo-(-0.9)) > 0.01 {
		t.Fatalf("ожидали минимальный lo-пик ~-0.9, получили %.4f", minLo)
	}
}

func TestPeaksUnsupportedFormat(t *testing.T) {
	// .m4a не поддерживается декодером — должен вернуть пустой массив или ошибку.
	dir := t.TempDir()
	path := filepath.Join(dir, "track.m4a")
	if err := os.WriteFile(path, []byte("fakem4adata"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := PeakMinMax(path, 80)
	// Принимаем оба исхода: ошибку ИЛИ пустой массив.
	if err == nil && len(p) > 0 {
		t.Fatal("ожидали ошибку или пустой массив для неподдерживаемого формата")
	}
}

func TestPeaksBrokenWAV(t *testing.T) {
	// Файл с расширением .wav, но невалидными данными.
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.wav")
	if err := os.WriteFile(path, []byte("not a wav file at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := PeakMinMax(path, 80)
	if err == nil && len(p) > 0 {
		t.Fatal("ожидали ошибку или пустой массив для битого WAV")
	}
}

func TestPeaksMissingFile(t *testing.T) {
	_, err := PeakMinMax(filepath.Join(t.TempDir(), "nonexistent.wav"), 80)
	if err == nil {
		t.Fatal("ожидали ошибку для несуществующего файла")
	}
}

func TestPeakMinMaxBinsPartitioning(t *testing.T) {
	// Проверяем peakMinMaxBins непосредственно: min и max соответствуют окну.
	samples := make([]float64, 100)
	// Бин 0 (0..9): положительный пик 0.5; бин 1 (10..19): пик 0.8; остальные 0.
	samples[5] = 0.5
	samples[15] = 0.8

	out := peakMinMaxBins(samples, 10)
	if len(out) != 10 {
		t.Fatalf("ожидали 10 бинов, получили %d", len(out))
	}
	// Бин 0: min=0 (все остальные нули), max=0.5.
	if math.Abs(out[0][1]-0.5) > 0.001 {
		t.Fatalf("бин 0 max: ожидали 0.5, получили %.4f", out[0][1])
	}
	if out[0][0] != 0 {
		t.Fatalf("бин 0 min: ожидали 0, получили %.4f", out[0][0])
	}
	// Бин 1: min=0, max=0.8.
	if math.Abs(out[1][1]-0.8) > 0.001 {
		t.Fatalf("бин 1 max: ожидали 0.8, получили %.4f", out[1][1])
	}
	if out[1][0] != 0 {
		t.Fatalf("бин 1 min: ожидали 0, получили %.4f", out[1][0])
	}
	// Остальные бины: нулевые.
	for i := 2; i < 10; i++ {
		if out[i][0] != 0 || out[i][1] != 0 {
			t.Fatalf("бин %d: ожидали [0,0], получили [%.4f,%.4f]", i, out[i][0], out[i][1])
		}
	}
}

func TestPeakMinMaxBinsMinLessThanMax(t *testing.T) {
	// Шум должен давать min < max в каждом непустом бине.
	samples := noise(44100, 1.0, 99)

	out := peakMinMaxBins(samples, 200)
	for i, v := range out {
		if v[0] > v[1] {
			t.Fatalf("бин %d: min %.4f > max %.4f", i, v[0], v[1])
		}
	}
}

func TestPeaksEmptySamples(t *testing.T) {
	// Пустой вход — все бины должны быть [0, 0].
	out := peakMinMaxBins([]float64{}, 10)
	if len(out) != 10 {
		t.Fatalf("ожидали 10 бинов, получили %d", len(out))
	}
	for i, v := range out {
		if v[0] != 0 || v[1] != 0 {
			t.Fatalf("бин %d: пустой вход должен давать [0,0], получили [%.4f,%.4f]", i, v[0], v[1])
		}
	}
}
