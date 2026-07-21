// Analysis throughput benchmark for the OLD Go harvest pipeline, for a
// head-to-head against the native Rust bench (flapp-dsp examples/bench.rs).
//
//   cd backend && go run ./cmd/bench "C:\path\to\samples"
//
// Times AnalyzeAll (decode + spectral features + perceptual hash) over every
// audio file, parallel across GOMAXPROCS workers — the per-file work the old
// Sounds harvest does. NOTE: not the same feature set as the native pipeline
// (which computes BPM/key/waveform peaks); decode cost is the shared dominant
// term. Reports file count and files/sec.
package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flapp/core/internal/infrastructure/audio"
)

func isAudio(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".wav", ".mp3", ".flac", ".ogg", ".aiff", ".aif", ".m4a", ".aac":
		return true
	}
	return false
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: bench <folder>")
		os.Exit(1)
	}
	dir := os.Args[1]

	fmt.Printf("scanning %s …\n", dir)
	var paths []string
	t := time.Now()
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && isAudio(d.Name()) {
			paths = append(paths, p)
		}
		return nil
	})
	scanS := time.Since(t).Seconds()
	n := len(paths)
	fmt.Printf("scan: %d audio files in %.3fs\n", n, scanS)
	if n == 0 {
		return
	}

	a := audio.NewAnalyzer()
	workers := runtime.NumCPU()
	ctx := context.Background()

	var done int64
	jobs := make(chan string, n)
	for _, p := range paths {
		jobs <- p
	}
	close(jobs)

	t = time.Now()
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				_, _, _ = a.AnalyzeAll(ctx, p)
				atomic.AddInt64(&done, 1)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(t).Seconds()

	fps := float64(n) / elapsed
	perMs := elapsed / float64(n) * 1000.0
	fmt.Printf("\nGo harvest AnalyzeAll (%d workers)\n", workers)
	fmt.Printf("%-24s %7.3fs   %8.1f files/s   %6.2f ms/file\n", "cold parallel", elapsed, fps, perMs)
}
