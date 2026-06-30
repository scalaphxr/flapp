package archive

import (
	"errors"
	"fmt"
	"io"
)

const (
	// BombMaxRatio is the maximum allowed uncompressed/compressed size ratio
	// per entry. ZIP bombs typically achieve ratios of 1000:1 or higher.
	BombMaxRatio = 100.0

	// BombMaxTotalBytes is the absolute cap on total bytes extracted from a
	// single archive tree (including nested archives). 2 GB by default.
	BombMaxTotalBytes int64 = 2 << 30

	// MaxNestedDepth is the maximum recursion depth when extracting nested
	// archives. FL Studio packs rarely exceed depth 2; depth 3 is generous.
	MaxNestedDepth = 3
)

// ErrZipBomb is returned when extraction limits are exceeded.
var ErrZipBomb = errors.New("archive: extraction limit exceeded (possible zip bomb)")

// extractionTracker is shared state for a single archive extraction tree.
// It counts bytes across all entries and nested archives so the absolute cap
// is enforced at the tree level, not per-entry.
type extractionTracker struct {
	totalExtracted int64
	maxTotal       int64
}

func newTracker() *extractionTracker {
	return &extractionTracker{maxTotal: BombMaxTotalBytes}
}

// bombGuardReader wraps an io.Reader and enforces the per-entry size ratio
// and the shared absolute cap.
type bombGuardReader struct {
	r          io.Reader
	compSize   int64
	maxRatio   float64
	tracker    *extractionTracker
	entryRead  int64
}

func newBombGuardReader(r io.Reader, compressedSize int64, maxRatio float64, tr *extractionTracker) *bombGuardReader {
	if maxRatio <= 0 {
		maxRatio = BombMaxRatio
	}
	return &bombGuardReader{
		r:        r,
		compSize: compressedSize,
		maxRatio: maxRatio,
		tracker:  tr,
	}
}

func (bg *bombGuardReader) Read(p []byte) (int, error) {
	n, err := bg.r.Read(p)
	if n > 0 {
		bg.entryRead += int64(n)
		bg.tracker.totalExtracted += int64(n)

		// Absolute cap.
		if bg.tracker.totalExtracted > bg.tracker.maxTotal {
			return n, fmt.Errorf("%w: total bytes %.1f MB exceeds limit %.1f MB",
				ErrZipBomb,
				float64(bg.tracker.totalExtracted)/(1<<20),
				float64(bg.tracker.maxTotal)/(1<<20),
			)
		}

		// Per-entry ratio check (only after we have meaningful data; compSize=0
		// means unknown, skip ratio check).
		if bg.compSize > 0 && bg.entryRead > 4096 {
			ratio := float64(bg.entryRead) / float64(bg.compSize)
			if ratio > bg.maxRatio {
				return n, fmt.Errorf("%w: entry ratio %.1f exceeds limit %.1f",
					ErrZipBomb, ratio, bg.maxRatio)
			}
		}
	}
	return n, err
}

// isZipBombErr reports whether err was raised by bomb protection.
func isZipBombErr(err error) bool {
	return errors.Is(err, ErrZipBomb)
}
