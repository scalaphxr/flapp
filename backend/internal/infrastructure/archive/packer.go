package archive

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/flapp/core/internal/domain"
)

// Packer implements domain.Packer, producing .zip sample packs. The 7z/rar
// write paths are intentionally not implemented: zip is the universal, lossless
// container every DAW and OS opens natively, so exported kits stay portable.
type Packer struct{}

// NewPacker returns a zip packer.
func NewPacker() *Packer { return &Packer{} }

// Pack writes entries into a single .zip at dest. format is accepted for
// interface symmetry; anything other than "zip" is rejected with a clear error
// rather than silently producing the wrong container.
func (p *Packer) Pack(ctx context.Context, dest, format string, entries []domain.PackEntry, progress func(done, total int)) error {
	if f := strings.ToLower(strings.TrimPrefix(format, ".")); f != "" && f != "zip" {
		return fmt.Errorf("%w: pack export supports zip only, got %q", domain.ErrUnsupported, format)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	zw := zip.NewWriter(out)
	total := len(entries)
	used := make(map[string]int) // de-collide identical arc paths

	for i, entry := range entries {
		if err := ctx.Err(); err != nil {
			zw.Close()
			return err
		}
		if err := p.addOne(zw, entry, used); err != nil {
			// Skip a missing/unreadable source rather than aborting the pack.
			continue
		}
		if progress != nil {
			progress(i+1, total)
		}
	}
	return zw.Close()
}

// addOne copies a single source file into the zip under a unique arc path.
func (p *Packer) addOne(zw *zip.Writer, entry domain.PackEntry, used map[string]int) error {
	src, err := os.Open(entry.SourcePath)
	if err != nil {
		return err
	}
	defer src.Close()

	arc := uniqueArcPath(cleanArcPath(entry.ArcPath), used)
	w, err := zw.Create(arc)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, src)
	return err
}

// cleanArcPath normalises an in-archive path to forward slashes and removes any
// leading separators or traversal segments.
func cleanArcPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = path.Clean("/" + p)
	return strings.TrimPrefix(p, "/")
}

// uniqueArcPath appends " (n)" before the extension when an arc path repeats.
func uniqueArcPath(p string, used map[string]int) string {
	n, seen := used[p]
	if !seen {
		used[p] = 0
		return p
	}
	n++
	used[p] = n
	ext := path.Ext(p)
	stem := strings.TrimSuffix(p, ext)
	return fmt.Sprintf("%s (%d)%s", stem, n, ext)
}
