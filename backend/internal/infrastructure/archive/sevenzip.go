package archive

import (
	"context"

	"github.com/bodgit/sevenzip"
	"github.com/flapp/core/internal/domain"
)

// extract7z handles .7z archives via bodgit/sevenzip, whose API mirrors the
// standard library's archive/zip reader.
func (e *Extractor) extract7z(ctx context.Context, archivePath, destDir string, fn func(domain.ExtractedFile) error) error {
	r, err := sevenzip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	counter := 0
	for _, f := range r.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if f.FileInfo().IsDir() || !e.wants(f.Name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
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
