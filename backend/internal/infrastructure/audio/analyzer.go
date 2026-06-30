package audio

import (
	"context"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/flapp/core/internal/domain"
)

// subStats накапливает CPU-время под-стадий анализа (потокобезопасно через atomic).
type subStats struct {
	decodeWAVNs   atomic.Int64 // декод WAV/AIFF
	decodeMP3Ns   atomic.Int64 // декод MP3
	countWAV      atomic.Int64 // кол-во WAV/AIFF файлов декодировано
	countMP3      atomic.Int64 // кол-во MP3 декодировано
	sampleCount   atomic.Int64 // суммарное кол-во моно-сэмплов декодировано
	timeDomainNs  atomic.Int64 // RMS / ZCR / peak / attack pass
	loudestWinNs  atomic.Int64 // поиск громкого окна (loudestWindow scan)
	spectralFFTNs atomic.Int64 // спектральный анализ (FFT + centroid + band energy)
	fingerprintNs atomic.Int64 // перцептивный отпечаток (16×FFT + bit extraction)
}

// SubStatsSnap — снимок under-stage таймингов для отчёта.
type SubStatsSnap struct {
	DecodeWAVNs   int64
	DecodeMP3Ns   int64
	CountWAV      int64
	CountMP3      int64
	SampleCount   int64
	TimeDomainNs  int64
	LoudestWinNs  int64
	SpectralFFTNs int64
	FingerprintNs int64
}

// SubStats возвращает накопленный снимок под-стадийных таймингов.
func (a *Analyzer) SubStats() SubStatsSnap {
	return SubStatsSnap{
		DecodeWAVNs:   a.sub.decodeWAVNs.Load(),
		DecodeMP3Ns:   a.sub.decodeMP3Ns.Load(),
		CountWAV:      a.sub.countWAV.Load(),
		CountMP3:      a.sub.countMP3.Load(),
		SampleCount:   a.sub.sampleCount.Load(),
		TimeDomainNs:  a.sub.timeDomainNs.Load(),
		LoudestWinNs:  a.sub.loudestWinNs.Load(),
		SpectralFFTNs: a.sub.spectralFFTNs.Load(),
		FingerprintNs: a.sub.fingerprintNs.Load(),
	}
}

// Analyzer implements domain.AudioAnalyzer. It decodes WAV/AIFF natively for
// full signal analysis and probes compressed containers for duration/format.
type Analyzer struct {
	// MaxAnalysisSamples bounds how much PCM is read for feature extraction
	// (keeps very long files cheap). 0 means "read everything".
	MaxAnalysisSamples int

	sub subStats // под-стадийные тайминги; читается через SubStats()
}

// NewAnalyzer returns an Analyzer that reads up to ~30 s at 48 kHz per file.
func NewAnalyzer() *Analyzer {
	return &Analyzer{MaxAnalysisSamples: 48000 * 30}
}

const (
	fpTimeBins = 16
	fpBands    = 32
)

// Analyze extracts metadata and signal features.
func (a *Analyzer) Analyze(ctx context.Context, path string) (domain.AudioFeatures, error) {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	feat := domain.AudioFeatures{}

	if ext == "wav" || ext == "aiff" || ext == "aif" || ext == "mp3" {
		p, err := a.decode(path, ext)
		if err == nil && p != nil && len(p.samples) > 0 {
			feat = a.featuresFromPCM(p)
			feat.Analyzed = true
			return feat, nil
		}
		// fall through to header probe on decode failure
	}

	sr, ch, dur, err := probeCompressed(path, ext)
	if err == nil {
		feat.SampleRate = sr
		feat.Channels = ch
		feat.DurationSeconds = dur
		feat.Analyzed = false // signal features unavailable without decoding
		return feat, nil
	}
	return feat, nil // unknown format: leave zeroed, never error the pipeline
}

func (a *Analyzer) decode(path, ext string) (*pcm, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	switch ext {
	case "wav":
		return decodeWAV(f, a.MaxAnalysisSamples)
	case "aiff", "aif":
		return decodeAIFF(f, a.MaxAnalysisSamples)
	case "mp3":
		return decodeMP3(f, a.MaxAnalysisSamples)
	}
	return nil, errNoPCM
}

func (a *Analyzer) featuresFromPCM(p *pcm) domain.AudioFeatures {
	f := domain.AudioFeatures{
		SampleRate:      p.sampleRate,
		Channels:        p.channels,
		BitDepth:        p.bitDepth,
		DurationSeconds: float64(len(p.samples)) / float64(max1(p.sampleRate)),
	}

	// --- time-domain descriptors (RMS, ZCR, peak, attack, crest, onsets) ---
	t0 := time.Now()
	var sumSq, peak float64
	zc := 0
	prev := 0.0
	for i, s := range p.samples {
		sumSq += s * s
		if abs(s) > peak {
			peak = abs(s)
		}
		if i > 0 && ((s >= 0) != (prev >= 0)) {
			zc++
		}
		prev = s
	}
	n := float64(len(p.samples))
	f.RMS = math.Sqrt(sumSq / n)
	f.PeakAmplitude = peak
	f.ZeroCrossRate = float64(zc) / n
	if peak > 0 {
		thr := 0.9 * peak
		for i, s := range p.samples {
			if abs(s) >= thr {
				f.AttackTime = float64(i) / float64(max1(p.sampleRate))
				break
			}
		}
		if f.RMS > 0 {
			f.CrestFactor = peak / f.RMS
		}
	}

	// OnsetCount: count of 10ms windows where RMS rises by >30% of peak-window RMS.
	winSz := max1(p.sampleRate / 100)
	var peakWinRMS float64
	for i := 0; i+winSz <= len(p.samples); i += winSz {
		if e := rmsOfSlice(p.samples[i : i+winSz]); e > peakWinRMS {
			peakWinRMS = e
		}
	}
	if peakWinRMS > 0 {
		threshold := peakWinRMS * 0.3
		prevWin := 0.0
		for i := 0; i+winSz <= len(p.samples); i += winSz {
			cur := rmsOfSlice(p.samples[i : i+winSz])
			if cur-prevWin > threshold {
				f.OnsetCount++
			}
			prevWin = cur
		}
	}
	a.sub.timeDomainNs.Add(time.Since(t0).Nanoseconds())

	// --- frequency-domain descriptors via one windowed FFT ---
	centroid, low, high, subBass, flat, decay := a.spectralDescriptors(p)
	f.SpectralCentroid = centroid
	f.LowEnergyRatio = low
	f.HighEnergyRatio = high
	f.SubBassRatio = subBass
	f.SpectralFlatness = flat
	f.DecayRate = decay
	return f
}

func (a *Analyzer) spectralDescriptors(p *pcm) (centroid, lowRatio, highRatio, subBassRatio, flatness, decayRate float64) {
	win := 4096
	if len(p.samples) < win {
		win = nextPow2(len(p.samples)) / 2
		if win < 256 {
			win = len(p.samples)
		}
	}
	win = nextPow2(win)
	if win > len(p.samples) {
		win = nextPow2(len(p.samples)/2 + 1)
	}
	if win < 64 {
		return 0, 0, 0, 0, 0, 0
	}
	// Поиск громкого окна (O(n) по всем сэмплам) — тайминг отдельно.
	t0 := time.Now()
	startBest := loudestWindow(p.samples, win)
	a.sub.loudestWinNs.Add(time.Since(t0).Nanoseconds())

	// DecayRate: seconds from peak window to reach 50% RMS energy.
	winSz := max1(p.sampleRate / 100) // 10ms windows
	if startBest+winSz <= len(p.samples) {
		peakE := rmsOfSlice(p.samples[startBest : startBest+winSz])
		halfE := peakE * 0.5
		// Default: never decays (use full remaining duration).
		decayRate = float64(len(p.samples)-startBest) / float64(max1(p.sampleRate))
		for i := startBest + winSz; i+winSz <= len(p.samples); i += winSz {
			if rmsOfSlice(p.samples[i:i+winSz]) <= halfE {
				decayRate = float64(i-startBest) / float64(max1(p.sampleRate))
				break
			}
		}
	}

	// Применение окна Ханна + один FFT + спектральные метрики.
	frame := make([]float64, win)
	copy(frame, p.samples[startBest:startBest+win])
	w := hann(win)
	re := make([]float64, win)
	im := make([]float64, win)
	for i := 0; i < win; i++ {
		re[i] = frame[i] * w[i]
	}
	t1 := time.Now()
	fft(re, im)

	var magSum, weighted, lowE, highE, subE, totalE float64
	var logSum float64
	logCount := 0
	binHz := float64(p.sampleRate) / float64(win)
	for k := 1; k < win/2; k++ {
		mag := math.Hypot(re[k], im[k])
		freq := float64(k) * binHz
		magSum += mag
		weighted += mag * freq
		totalE += mag
		if freq < 80 {
			subE += mag
		}
		if freq < 150 {
			lowE += mag
		}
		if freq > 6000 {
			highE += mag
		}
		if mag > 1e-10 {
			logSum += math.Log(mag)
			logCount++
		}
	}
	a.sub.spectralFFTNs.Add(time.Since(t1).Nanoseconds())

	if magSum > 0 {
		centroid = weighted / magSum
	}
	if totalE > 0 {
		lowRatio = lowE / totalE
		highRatio = highE / totalE
		subBassRatio = subE / totalE
	}
	// SpectralFlatness = geometric mean / arithmetic mean of magnitudes.
	if logCount > 0 && magSum > 0 {
		flatness = math.Exp(logSum/float64(logCount)) / (magSum / float64(logCount))
	}
	return
}

// rmsOfSlice returns the root-mean-square energy of a sample slice.
func rmsOfSlice(s []float64) float64 {
	if len(s) == 0 {
		return 0
	}
	var sum float64
	for _, v := range s {
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(s)))
}

// Fingerprint produces a perceptual hash (hex) for WAV/AIFF. Compressed formats
// return "" — those rely on the content-hash dedup layer instead.
func (a *Analyzer) Fingerprint(ctx context.Context, path string) (string, error) {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if ext != "wav" && ext != "aiff" && ext != "aif" {
		return "", nil
	}
	p, err := a.decode(path, ext)
	if err != nil || p == nil || len(p.samples) < 256 {
		return "", nil
	}
	return a.perceptualHash(p), nil
}

// AnalyzeAll decodes the file exactly once and returns both signal features and
// the perceptual fingerprint. It is equivalent to calling Analyze followed by
// Fingerprint but avoids the second decode for WAV/AIFF files.
func (a *Analyzer) AnalyzeAll(ctx context.Context, path string) (domain.AudioFeatures, string, error) {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	feat := domain.AudioFeatures{}

	if ext == "wav" || ext == "aiff" || ext == "aif" || ext == "mp3" {
		t0 := time.Now()
		p, err := a.decode(path, ext)
		decNs := time.Since(t0).Nanoseconds()

		if err == nil && p != nil && len(p.samples) > 0 {
			// Учитываем тайминги декода по формату.
			switch ext {
			case "wav", "aiff", "aif":
				a.sub.decodeWAVNs.Add(decNs)
				a.sub.countWAV.Add(1)
			case "mp3":
				a.sub.decodeMP3Ns.Add(decNs)
				a.sub.countMP3.Add(1)
			}
			a.sub.sampleCount.Add(int64(len(p.samples)))

			feat = a.featuresFromPCM(p)
			feat.Analyzed = true
			fp := ""
			if ext != "mp3" && len(p.samples) >= 256 {
				t1 := time.Now()
				fp = a.perceptualHash(p)
				a.sub.fingerprintNs.Add(time.Since(t1).Nanoseconds())
			}
			return feat, fp, nil
		}
	}

	sr, ch, dur, err := probeCompressed(path, ext)
	if err == nil {
		feat.SampleRate = sr
		feat.Channels = ch
		feat.DurationSeconds = dur
	}
	return feat, "", nil
}

// perceptualHash: a chromaprint-style fingerprint. The signal is split into
// fpTimeBins time segments; each segment's spectrum is collapsed into fpBands
// log-spaced energy bands; a bit is set per adjacent-band comparison. The
// resulting bit grid is robust to volume and format changes, so Hamming
// distance between two fingerprints measures acoustic similarity.
func (a *Analyzer) perceptualHash(p *pcm) string {
	segLen := len(p.samples) / fpTimeBins
	if segLen < 64 {
		segLen = len(p.samples)
	}
	win := nextPow2(segLen)
	if win > 8192 {
		win = 8192
	}
	w := hann(win)

	// Pre-allocate all buffers once — zero allocations inside the loop.
	re := make([]float64, win)
	im := make([]float64, win)
	// Flat grid: fpTimeBins×fpBands contiguous floats; grid[t] = slice into it.
	gridFlat := make([]float64, fpTimeBins*fpBands)
	grid := make([][]float64, fpTimeBins)
	for t := range grid {
		grid[t] = gridFlat[t*fpBands : (t+1)*fpBands]
	}

	for t := 0; t < fpTimeBins; t++ {
		start := t * segLen
		if start+win > len(p.samples) {
			start = len(p.samples) - win
			if start < 0 {
				start = 0
			}
		}
		for i := range re {
			re[i] = 0
			im[i] = 0
		}
		end := start + win
		if end > len(p.samples) {
			end = len(p.samples)
		}
		for i := 0; start+i < end; i++ {
			re[i] = p.samples[start+i] * w[i]
		}
		fft(re, im)
		logBandsInto(re, im, win, p.sampleRate, grid[t])
	}

	// build bits: compare each band against the next within a time bin
	bits := make([]byte, (fpTimeBins*(fpBands-1)+7)/8)
	bitIdx := 0
	for t := 0; t < fpTimeBins; t++ {
		for b := 0; b < fpBands-1; b++ {
			if grid[t][b] > grid[t][b+1] {
				bits[bitIdx/8] |= 1 << uint(bitIdx%8)
			}
			bitIdx++
		}
	}
	return hex.EncodeToString(bits)
}

// logBands collapses an FFT magnitude spectrum into n log-spaced energy bands.
func logBands(re, im []float64, win, sampleRate, n int) []float64 {
	dst := make([]float64, n)
	logBandsInto(re, im, win, sampleRate, dst)
	return dst
}

// logBandsInto accumulates magnitudes into dst (caller zeroes it before each use)
// and applies log1p in-place. Returns dst so callers can chain.
func logBandsInto(re, im []float64, win, sampleRate int, dst []float64) {
	n := len(dst)
	half := win / 2
	minF := 80.0
	maxF := float64(sampleRate) / 2
	if maxF <= minF {
		maxF = minF * 2
	}
	logMin := math.Log(minF)
	logMax := math.Log(maxF)
	binHz := float64(sampleRate) / float64(win)
	for k := 1; k < half; k++ {
		freq := float64(k) * binHz
		if freq < minF || freq > maxF {
			continue
		}
		mag := math.Hypot(re[k], im[k])
		idx := int(float64(n) * (math.Log(freq) - logMin) / (logMax - logMin))
		if idx < 0 {
			idx = 0
		}
		if idx >= n {
			idx = n - 1
		}
		dst[idx] += mag * mag
	}
	for i, v := range dst {
		dst[i] = math.Log1p(v)
	}
}

func loudestWindow(s []float64, win int) int {
	if len(s) <= win {
		return 0
	}
	step := win / 4
	if step < 1 {
		step = 1
	}
	bestStart, bestE := 0, -1.0
	for start := 0; start+win <= len(s); start += step {
		var e float64
		for i := start; i < start+win; i++ {
			e += s[i] * s[i]
		}
		if e > bestE {
			bestE = e
			bestStart = start
		}
	}
	return bestStart
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func max1(x int) int {
	if x <= 0 {
		return 1
	}
	return x
}

// HammingHex returns the bit distance between two equal-length hex fingerprints.
// Returns -1 if they are not comparable.
func HammingHex(a, b string) int {
	if a == "" || b == "" || len(a) != len(b) {
		return -1
	}
	ab, err1 := hex.DecodeString(a)
	bb, err2 := hex.DecodeString(b)
	if err1 != nil || err2 != nil {
		return -1
	}
	dist := 0
	for i := range ab {
		dist += popcount(ab[i] ^ bb[i])
	}
	return dist
}

func popcount(b byte) int {
	c := 0
	for b != 0 {
		c += int(b & 1)
		b >>= 1
	}
	return c
}
