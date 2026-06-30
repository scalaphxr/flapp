package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/flapp/core/internal/domain"
)

// AnalysisCacheRepo persists content-addressed audio analysis results.
// Key: SHA-256 of file content.  Value: AudioFeatures + perceptual fingerprint.
// Re-running harvest over the same audio bytes skips the expensive decode+FFT.
type AnalysisCacheRepo struct{ db *sql.DB }

// GetCached returns the cached features and fingerprint for contentHash, or
// (zero, "", false) if not cached.
func (r *AnalysisCacheRepo) GetCached(ctx context.Context, contentHash string) (domain.AudioFeatures, string, bool) {
	var featJSON, fp string
	err := r.db.QueryRowContext(ctx,
		`SELECT features_json, fingerprint FROM analysis_cache WHERE content_hash=?`,
		contentHash,
	).Scan(&featJSON, &fp)
	if err != nil {
		return domain.AudioFeatures{}, "", false
	}
	var feat domain.AudioFeatures
	if err := json.Unmarshal([]byte(featJSON), &feat); err != nil {
		return domain.AudioFeatures{}, "", false
	}
	return feat, fp, true
}

// SetCached stores features + fingerprint for contentHash.
func (r *AnalysisCacheRepo) SetCached(ctx context.Context, contentHash string, feat domain.AudioFeatures, fp string) error {
	b, err := json.Marshal(feat)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO analysis_cache(content_hash,features_json,fingerprint,analyzed_at)
		 VALUES(?,?,?,?)
		 ON CONFLICT(content_hash) DO UPDATE SET
		   features_json=excluded.features_json,
		   fingerprint=excluded.fingerprint,
		   analyzed_at=excluded.analyzed_at`,
		contentHash, string(b), fp, time.Now().Unix(),
	)
	return err
}

// HarvestIndexRepo manages the Phase-1 fast-index table.
type HarvestIndexRepo struct{ db *sql.DB }

// IndexEntry is one row in harvest_index.
type IndexEntry struct {
	ID          int64
	Path        string
	MTime       int64
	Size        int64
	QuickHash   string
	ContentHash string
	Fmt         string
	Status      string // new | indexed | deep | done | skip
	ErrorMsg    string
}

// Upsert inserts or updates an index entry (keyed by path).
func (r *HarvestIndexRepo) Upsert(ctx context.Context, e *IndexEntry) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO harvest_index(path,mtime,size,quick_hash,content_hash,fmt,status,error_msg,indexed_at)
		 VALUES(?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(path) DO UPDATE SET
		   mtime=excluded.mtime, size=excluded.size,
		   quick_hash=excluded.quick_hash, content_hash=excluded.content_hash,
		   fmt=excluded.fmt, status=excluded.status,
		   error_msg=excluded.error_msg, indexed_at=excluded.indexed_at`,
		e.Path, e.MTime, e.Size,
		e.QuickHash, e.ContentHash, e.Fmt,
		e.Status, e.ErrorMsg, time.Now().Unix(),
	)
	return err
}

// GetByPath returns the index entry for path, if it exists.
func (r *HarvestIndexRepo) GetByPath(ctx context.Context, path string) (*IndexEntry, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id,path,mtime,size,quick_hash,content_hash,fmt,status,error_msg
		 FROM harvest_index WHERE path=?`, path)
	e := &IndexEntry{}
	err := row.Scan(&e.ID, &e.Path, &e.MTime, &e.Size,
		&e.QuickHash, &e.ContentHash, &e.Fmt, &e.Status, &e.ErrorMsg)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return e, err
}

// UpdateStatus sets the status (and optional error message) for a path.
func (r *HarvestIndexRepo) UpdateStatus(ctx context.Context, path, status, errMsg string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE harvest_index SET status=?, error_msg=?, indexed_at=? WHERE path=?`,
		status, errMsg, time.Now().Unix(), path)
	return err
}

// CheckpointRepo stores per-entry progress for large archive batch processing.
type CheckpointRepo struct{ db *sql.DB }

// MarkDone records that a specific archive entry has been processed.
func (r *CheckpointRepo) MarkDone(ctx context.Context, jobID, archivePath, entryName string, crc, size int64) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO harvest_checkpoint(job_id,archive_path,entry_name,entry_crc,entry_size,status,updated_at)
		 VALUES(?,?,?,?,?,'done',?)
		 ON CONFLICT(job_id,archive_path,entry_name) DO UPDATE SET status='done', updated_at=excluded.updated_at`,
		jobID, archivePath, entryName, crc, size, time.Now().Unix(),
	)
	return err
}

// IsDone reports whether an entry has already been processed in this job.
func (r *CheckpointRepo) IsDone(ctx context.Context, jobID, archivePath, entryName string) bool {
	var s string
	err := r.db.QueryRowContext(ctx,
		`SELECT status FROM harvest_checkpoint WHERE job_id=? AND archive_path=? AND entry_name=?`,
		jobID, archivePath, entryName).Scan(&s)
	return err == nil && s == "done"
}

// ClearJob removes all checkpoint rows for a given job.
func (r *CheckpointRepo) ClearJob(ctx context.Context, jobID string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM harvest_checkpoint WHERE job_id=?`, jobID)
	return err
}

// UnresolvedRepo stores FLP sample-path references that couldn't be resolved.
type UnresolvedRepo struct{ db *sql.DB }

// Record writes one unresolved asset reference.
func (r *UnresolvedRepo) Record(ctx context.Context, projectID int64, rawPath, normalized string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO unresolved_assets(project_id,raw_path,normalized,created_at)
		 VALUES(?,?,?,?)`,
		projectID, rawPath, normalized, time.Now().Unix(),
	)
	return err
}

// ListForProject returns all unresolved paths for a project.
func (r *UnresolvedRepo) ListForProject(ctx context.Context, projectID int64) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT raw_path FROM unresolved_assets WHERE project_id=? ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err == nil {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}
