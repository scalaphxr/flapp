package audio

import "math"

// fft computes the in-place radix-2 Cooley-Tukey FFT of a complex slice whose
// length must be a power of two. It is small, dependency-free, and fast enough
// for the analysis window sizes used here (<= 8192).
func fft(re, im []float64) {
	n := len(re)
	if n <= 1 {
		return
	}
	// bit-reversal permutation
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		ang := -2 * math.Pi / float64(length)
		wlenRe, wlenIm := math.Cos(ang), math.Sin(ang)
		for i := 0; i < n; i += length {
			wRe, wIm := 1.0, 0.0
			for k := 0; k < length/2; k++ {
				uRe, uIm := re[i+k], im[i+k]
				vRe := re[i+k+length/2]*wRe - im[i+k+length/2]*wIm
				vIm := re[i+k+length/2]*wIm + im[i+k+length/2]*wRe
				re[i+k], im[i+k] = uRe+vRe, uIm+vIm
				re[i+k+length/2], im[i+k+length/2] = uRe-vRe, uIm-vIm
				wRe, wIm = wRe*wlenRe-wIm*wlenIm, wRe*wlenIm+wIm*wlenRe
			}
		}
	}
}

// nextPow2 returns the smallest power of two >= n (min 1).
func nextPow2(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// hann returns a Hann window of length n.
func hann(n int) []float64 {
	w := make([]float64, n)
	if n == 1 {
		w[0] = 1
		return w
	}
	for i := range w {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
	}
	return w
}
