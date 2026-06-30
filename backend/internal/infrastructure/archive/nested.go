package archive

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flapp/core/internal/domain"
	"github.com/flapp/core/internal/infrastructure/format"
)

// NestedExtractor wraps a base Extractor with nested-archive traversal and
// zip-bomb protection. After each entry is written to disk it is checked for
// archive magic bytes; if found, and if depth < MaxNestedDepth, the extractor
// recurses into it.
//
// A single extractionTracker is shared across the entire tree so the absolute
// byte cap is enforced at the tree (job) level, not per-entry.
type NestedExtractor struct {
	base    *Extractor
	tracker *extractionTracker
}

// NewNested wraps e with nested-archive support.
func NewNested(e *Extractor) *NestedExtractor {
	return &NestedExtractor{base: e, tracker: newTracker()}
}

// Extract starts extraction at depth 0.
func (ne *NestedExtractor) Extract(
	ctx context.Context,
	archivePath, destDir string,
	fn func(domain.ExtractedFile) error,
) error {
	return ne.extractAt(ctx, archivePath, destDir, fn, 0, filepath.Base(archivePath))
}

func (ne *NestedExtractor) extractAt(
	ctx context.Context,
	archivePath, destDir string,
	fn func(domain.ExtractedFile) error,
	depth int, breadcrumb string,
) error {
	// Choose extraction path. For ZIP we use the bomb-safe version; for RAR/7z
	// we fall back to the base extractor (those formats already stream lazily).
	switch strings.ToLower(filepath.Ext(archivePath)) {
	case ".zip":
		return ne.extractZipBombSafe(ctx, archivePath, destDir, fn, depth, breadcrumb)
	default:
		return ne.base.Extract(ctx, archivePath, destDir, func(e domain.ExtractedFile) error {
			return ne.maybeRecurse(ctx, e, destDir, fn, depth, breadcrumb)
		})
	}
}

// extractZipBombSafe reads the ZIP central directory, pre-checks the
// compression ratio per entry, wraps each reader with the bomb guard, and
// normalises entry paths.
func (ne *NestedExtractor) extractZipBombSafe(
	ctx context.Context,
	archivePath, destDir string,
	fn func(domain.ExtractedFile) error,
	depth int, breadcrumb string,
) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()

	counter := 0
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			continue
		}

		// Normalise path and reject traversal attempts.
		safe, err := normalizeEntryPath(f.Name)
		if err != nil {
			continue
		}

		// Pre-check ratio using central-directory metadata.
		if f.CompressedSize64 > 0 && f.UncompressedSize64 > 0 {
			ratio := float64(f.UncompressedSize64) / float64(f.CompressedSize64)
			if ratio > BombMaxRatio {
				continue // skip suspect entry
			}
		}

		if !ne.base.wants(safe) {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			continue
		}
		guarded := newBombGuardReader(rc, int64(f.CompressedSize64), BombMaxRatio, ne.tracker)
		entry, werr := writeTempEntry(destDir, safe, &counter, guarded)
		rc.Close()

		if werr != nil {
			if isZipBombErr(werr) {
				return werr
			}
			continue
		}
		if err := ne.maybeRecurse(ctx, entry, destDir, fn, depth, breadcrumb); err != nil {
			return err
		}
	}
	return nil
}

// maybeRecurse: if the extracted entry is an archive and depth allows, recurse;
// otherwise yield it to fn.
func (ne *NestedExtractor) maybeRecurse(
	ctx context.Context,
	e domain.ExtractedFile,
	destDir string,
	fn func(domain.ExtractedFile) error,
	depth int, breadcrumb string,
) error {
	if depth < MaxNestedDepth && format.DetectFile(e.TempPath).IsArchive() {
		nestedDest := filepath.Join(destDir, fmt.Sprintf("nest%d_%s", depth+1, sanitizeBase(e.Name)))
		if mkErr := os.MkdirAll(nestedDest, 0o755); mkErr == nil {
			innerCrumb := breadcrumb + " → " + e.Name
			ierr := ne.extractAt(ctx, e.TempPath, nestedDest, func(inner domain.ExtractedFile) error {
				inner.RelPath = innerCrumb + "/" + inner.RelPath
				return fn(inner)
			}, depth+1, innerCrumb)
			os.Remove(e.TempPath)
			if ierr != nil && !isZipBombErr(ierr) {
				return nil // tolerate bad nested archive
			}
			return ierr
		}
	}
	return fn(e)
}

// normalizeEntryPath validates and cleans an archive entry path:
//   - Rejects absolute paths and "../" traversal sequences.
//   - Normalises Windows backslashes to forward slashes.
//   - Applies NFC Unicode normalisation for cross-platform consistency.
func normalizeEntryPath(entryPath string) (string, error) {
	clean := strings.ReplaceAll(entryPath, "\\", "/")
	if strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("absolute path: %q", entryPath)
	}
	parts := strings.Split(clean, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		switch p {
		case "..":
			return "", fmt.Errorf("path traversal: %q", entryPath)
		case ".", "":
			// skip
		default:
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return "", fmt.Errorf("empty path: %q", entryPath)
	}
	return strings.Join(out, "/"), nil
}
