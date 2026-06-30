// Package archive implements the domain.ArchiveExtractor and domain.Packer
// ports. It can read .zip (standard library), .rar (nwaples/rardecode) and .7z
// (bodgit/sevenzip), and write .zip packs for the Sample Pack Builder.
//
// Extraction is streaming and bounded: each contained file is written to a
// flattened temp path inside destDir (never honouring the archive's own
// directory structure, which defeats "zip-slip" path-traversal attacks), then
// handed to a callback that may move or delete it. An optional extension
// allowlist means non-audio payloads are skipped without ever touching disk.
package archive

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/flapp/core/internal/domain"
)

// Extractor implements domain.ArchiveExtractor.
type Extractor struct {
	keep map[string]bool // lowercase extensions (with dot); empty = keep everything
}

// New builds an extractor. If keepExts is non-empty, only files whose extension
// is listed are extracted to disk; everything else is skipped. Pass nil to
// extract every entry.
func New(keepExts []string) *Extractor {
	var keep map[string]bool
	if len(keepExts) > 0 {
		keep = make(map[string]bool, len(keepExts))
		for _, e := range keepExts {
			keep[normalizeExt(e)] = true
		}
	}
	return &Extractor{keep: keep}
}

// supportedArchive lists the container formats this extractor dispatches on.
var supportedArchive = map[string]bool{".zip": true, ".rar": true, ".7z": true}

// Supports reports whether the given archive extension can be opened.
func (e *Extractor) Supports(ext string) bool {
	return supportedArchive[normalizeExt(ext)]
}

// Extract opens archivePath and streams each kept entry through fn.
func (e *Extractor) Extract(ctx context.Context, archivePath, destDir string, fn func(entry domain.ExtractedFile) error) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	switch strings.ToLower(filepath.Ext(archivePath)) {
	case ".zip":
		return e.extractZip(ctx, archivePath, destDir, fn)
	case ".rar":
		return e.extractRAR(ctx, archivePath, destDir, fn)
	case ".7z":
		return e.extract7z(ctx, archivePath, destDir, fn)
	default:
		return fmt.Errorf("%w: %s", domain.ErrUnsupported, filepath.Ext(archivePath))
	}
}

// wants reports whether an inner file with the given name should be extracted.
func (e *Extractor) wants(name string) bool {
	if len(e.keep) == 0 {
		return true
	}
	return e.keep[strings.ToLower(filepath.Ext(name))]
}

// extractZip handles the standard-library zip format.
func (e *Extractor) extractZip(ctx context.Context, archivePath, destDir string, fn func(domain.ExtractedFile) error) error {
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
		if f.FileInfo().IsDir() || !e.wants(f.Name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue // skip unreadable entries rather than aborting the whole archive
		}
		entry, err := writeTempEntry(destDir, f.Name, &counter, rc)
		rc.Close()
		if err != nil {
			continue
		}
		if err := fn(entry); err != nil {
			return err
		}
	}
	return nil
}

// writeTempEntry streams r into a unique flattened file under destDir and
// returns the ExtractedFile describing it. relName is the archive-internal path
// (kept for folder-based classification hints); the on-disk name is sanitised.
func writeTempEntry(destDir, relName string, counter *int, r io.Reader) (domain.ExtractedFile, error) {
	base := sanitizeBase(path.Base(filepath.ToSlash(relName)))
	*counter++
	tempName := fmt.Sprintf("%06d_%s", *counter, base)
	tempPath := filepath.Join(destDir, tempName)

	dst, err := os.Create(tempPath)
	if err != nil {
		return domain.ExtractedFile{}, err
	}
	n, err := io.Copy(dst, r)
	closeErr := dst.Close()
	if err != nil {
		os.Remove(tempPath)
		return domain.ExtractedFile{}, err
	}
	if closeErr != nil {
		os.Remove(tempPath)
		return domain.ExtractedFile{}, closeErr
	}
	return domain.ExtractedFile{
		Name:     base,
		RelPath:  filepath.ToSlash(relName),
		TempPath: tempPath,
		Size:     n,
	}, nil
}

// sanitizeBase strips anything that could escape destDir or be illegal on the
// target filesystem. The result is always a plain file name.
func sanitizeBase(name string) string {
	name = strings.ReplaceAll(name, "\x00", "")
	name = filepath.Base(name)
	if name == "." || name == ".." || name == "" || name == string(filepath.Separator) {
		return "file"
	}
	// Replace characters illegal on Windows so packs extracted there are valid.
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
	)
	return replacer.Replace(name)
}

// normalizeExt lowercases an extension and guarantees a leading dot.
func normalizeExt(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return ext
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}
