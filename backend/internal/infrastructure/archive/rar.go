package archive

import (
	"context"

	"github.com/nwaples/rardecode/v2"
	"github.com/flapp/core/internal/domain"
)

// extractRAR handles .rar archives via nwaples/rardecode. The reader yields one
// file at a time; the embedded io.Reader streams the current file's bytes.
func (e *Extractor) extractRAR(ctx context.Context, archivePath, destDir string, fn func(domain.ExtractedFile) error) error {
	rc, err := rardecode.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer rc.Close()

	counter := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := rc.Next()
		if err != nil {
			break // io.EOF or an unrecoverable archive error ends the walk
		}
		if hdr.IsDir || !e.wants(hdr.Name) {
			continue
		}
		entry, err := writeTempEntry(destDir, hdr.Name, &counter, rc)
		if err != nil {
			continue
		}
		if err := fn(entry); err != nil {
			return err
		}
	}
	return nil
}
