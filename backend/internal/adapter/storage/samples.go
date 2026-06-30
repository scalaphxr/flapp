package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/flapp/core/internal/domain"
	"github.com/flapp/core/internal/infrastructure/audio"
)

// SampleRepo implements domain.SampleRepository over SQLite.
type SampleRepo struct {
	db *sql.DB
}

// sampleColumns is the canonical SELECT column order consumed by scanSample.
const sampleColumns = `id, name, path, ext, size, category, auto, origin,
	source_label, source_path, md5, sha256, fingerprint, bpm, key_name,
	favorite, rating, used_count, tags_json, added_at, modified_at,
	sample_rate, channels, bit_depth, duration, rms, peak, centroid, zcr,
	low_ratio, high_ratio, attack, analyzed,
	spectral_flatness, crest_factor, decay_rate, onset_count, sub_bass_ratio`

// scanSample reads one row in sampleColumns order into a domain.Sample.
func scanSample(scan func(dest ...any) error) (*domain.Sample, error) {
	var (
		s         domain.Sample
		auto, fav int
		analyzed  int
		tagsJSON  string
		addedAt   int64
		modAt     int64
	)
	err := scan(
		&s.ID, &s.Name, &s.Path, &s.Ext, &s.Size, &s.Category, &auto, &s.Origin,
		&s.SourceLabel, &s.SourcePath, &s.MD5, &s.SHA256, &s.Fingerprint, &s.BPM, &s.KeyName,
		&fav, &s.Rating, &s.UsedCount, &tagsJSON, &addedAt, &modAt,
		&s.Features.SampleRate, &s.Features.Channels, &s.Features.BitDepth,
		&s.Features.DurationSeconds, &s.Features.RMS, &s.Features.PeakAmplitude,
		&s.Features.SpectralCentroid, &s.Features.ZeroCrossRate,
		&s.Features.LowEnergyRatio, &s.Features.HighEnergyRatio, &s.Features.AttackTime,
		&analyzed,
		&s.Features.SpectralFlatness, &s.Features.CrestFactor, &s.Features.DecayRate,
		&s.Features.OnsetCount, &s.Features.SubBassRatio,
	)
	if err != nil {
		return nil, err
	}
	s.Auto = auto != 0
	s.Favorite = fav != 0
	s.Features.Analyzed = analyzed != 0
	s.AddedAt = time.Unix(addedAt, 0)
	s.ModifiedAt = time.Unix(modAt, 0)
	if tagsJSON != "" {
		_ = json.Unmarshal([]byte(tagsJSON), &s.Tags)
	}
	if s.Tags == nil {
		s.Tags = []string{}
	}
	return &s, nil
}

// Upsert inserts or updates a sample (keyed by path) and synchronises its tag
// rows and the FTS index, all in one transaction.
func (r *SampleRepo) Upsert(ctx context.Context, s *domain.Sample) (int64, error) {
	if s.AddedAt.IsZero() {
		s.AddedAt = time.Now()
	}
	s.ModifiedAt = time.Now()
	if s.Tags == nil {
		s.Tags = []string{}
	}
	tagsJSON, _ := json.Marshal(s.Tags)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	const q = `
INSERT INTO samples (
    name, path, ext, size, category, auto, origin, source_label, source_path,
    md5, sha256, fingerprint, bpm, key_name, favorite, rating, used_count,
    tags_json, added_at, modified_at,
    sample_rate, channels, bit_depth, duration, rms, peak, centroid, zcr,
    low_ratio, high_ratio, attack, analyzed,
    spectral_flatness, crest_factor, decay_rate, onset_count, sub_bass_ratio
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(path) DO UPDATE SET
    name=excluded.name, ext=excluded.ext, size=excluded.size,
    category=excluded.category, auto=excluded.auto, origin=excluded.origin,
    source_label=excluded.source_label, source_path=excluded.source_path,
    md5=excluded.md5, sha256=excluded.sha256, fingerprint=excluded.fingerprint,
    bpm=excluded.bpm, key_name=excluded.key_name, tags_json=excluded.tags_json,
    modified_at=excluded.modified_at,
    sample_rate=excluded.sample_rate, channels=excluded.channels,
    bit_depth=excluded.bit_depth, duration=excluded.duration, rms=excluded.rms,
    peak=excluded.peak, centroid=excluded.centroid, zcr=excluded.zcr,
    low_ratio=excluded.low_ratio, high_ratio=excluded.high_ratio,
    attack=excluded.attack, analyzed=excluded.analyzed,
    spectral_flatness=excluded.spectral_flatness, crest_factor=excluded.crest_factor,
    decay_rate=excluded.decay_rate, onset_count=excluded.onset_count,
    sub_bass_ratio=excluded.sub_bass_ratio
RETURNING id`

	row := tx.QueryRowContext(ctx, q,
		s.Name, s.Path, s.Ext, s.Size, string(s.Category), boolToInt(s.Auto), string(s.Origin),
		s.SourceLabel, s.SourcePath, s.MD5, s.SHA256, s.Fingerprint, s.BPM, s.KeyName,
		boolToInt(s.Favorite), s.Rating, s.UsedCount, string(tagsJSON),
		s.AddedAt.Unix(), s.ModifiedAt.Unix(),
		s.Features.SampleRate, s.Features.Channels, s.Features.BitDepth,
		s.Features.DurationSeconds, s.Features.RMS, s.Features.PeakAmplitude,
		s.Features.SpectralCentroid, s.Features.ZeroCrossRate,
		s.Features.LowEnergyRatio, s.Features.HighEnergyRatio, s.Features.AttackTime,
		boolToInt(s.Features.Analyzed),
		s.Features.SpectralFlatness, s.Features.CrestFactor, s.Features.DecayRate,
		s.Features.OnsetCount, s.Features.SubBassRatio,
	)
	var id int64
	if err := row.Scan(&id); err != nil {
		return 0, err
	}
	s.ID = id

	if err := syncSampleTags(ctx, tx, id, s.Tags); err != nil {
		return 0, err
	}
	if err := syncSampleFTS(ctx, tx, id, s); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// syncSampleTags rewrites the normalised tag rows for a sample.
func syncSampleTags(ctx context.Context, tx *sql.Tx, id int64, tags []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM sample_tags WHERE sample_id=?`, id); err != nil {
		return err
	}
	if len(tags) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO sample_tags(sample_id, tag) VALUES(?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, t := range tags {
		if t == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, id, t); err != nil {
			return err
		}
	}
	return nil
}

// syncSampleFTS refreshes the full-text row (rowid = sample id).
func syncSampleFTS(ctx context.Context, tx *sql.Tx, id int64, s *domain.Sample) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM samples_fts WHERE rowid=?`, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO samples_fts(rowid, name, tags, category, source) VALUES(?,?,?,?,?)`,
		id, s.Name, strings.Join(s.Tags, " "), string(s.Category), s.SourceLabel,
	)
	return err
}

// GetPeaksJSON возвращает кешированный JSON массива пиков для сэмпла.
// Возвращает пустую строку, если пики ещё не были вычислены.
func (r *SampleRepo) GetPeaksJSON(ctx context.Context, id int64) (string, error) {
	var j string
	err := r.db.QueryRowContext(ctx, `SELECT peaks_json FROM samples WHERE id=?`, id).Scan(&j)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	return j, err
}

// SetPeaksJSON сохраняет JSON массива пиков для последующих быстрых запросов.
func (r *SampleRepo) SetPeaksJSON(ctx context.Context, id int64, j string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE samples SET peaks_json=? WHERE id=?`, j, id)
	return err
}

// GetPeaks2JSON возвращает закешированный JSON пар [min,max] пиков (формат v2).
func (r *SampleRepo) GetPeaks2JSON(ctx context.Context, id int64) (string, error) {
	var j string
	err := r.db.QueryRowContext(ctx, `SELECT peaks2_json FROM samples WHERE id=?`, id).Scan(&j)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	return j, err
}

// SetPeaks2JSON сохраняет JSON пар [min,max] пиков (формат v2) для быстрых запросов.
func (r *SampleRepo) SetPeaks2JSON(ctx context.Context, id int64, j string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE samples SET peaks2_json=? WHERE id=?`, j, id)
	return err
}

// GetByID fetches one sample.
func (r *SampleRepo) GetByID(ctx context.Context, id int64) (*domain.Sample, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+sampleColumns+` FROM samples WHERE id=?`, id)
	s, err := scanSample(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return s, err
}

// FindByHash returns the first sample matching the SHA-256 (preferred) or MD5.
func (r *SampleRepo) FindByHash(ctx context.Context, md5, sha256 string) (*domain.Sample, error) {
	if sha256 != "" {
		row := r.db.QueryRowContext(ctx, `SELECT `+sampleColumns+` FROM samples WHERE sha256=? LIMIT 1`, sha256)
		if s, err := scanSample(row.Scan); err == nil {
			return s, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	if md5 != "" {
		row := r.db.QueryRowContext(ctx, `SELECT `+sampleColumns+` FROM samples WHERE md5=? LIMIT 1`, md5)
		s, err := scanSample(row.Scan)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return s, err
	}
	return nil, domain.ErrNotFound
}

// FindByFingerprint scans stored fingerprints and returns the closest match
// within maxDistance (Hamming bits). SQLite cannot compute the bit distance, so
// candidates are compared in Go; for desktop-scale libraries this linear scan
// is fast enough and keeps the fingerprint format opaque to the database.
func (r *SampleRepo) FindByFingerprint(ctx context.Context, fp string, maxDistance int) (*domain.Sample, error) {
	if fp == "" {
		return nil, domain.ErrNotFound
	}
	id, ok, err := r.nearestFingerprintID(ctx, fp, maxDistance, 0)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, domain.ErrNotFound
	}
	return r.GetByID(ctx, id)
}

// nearestFingerprintID returns the id whose fingerprint is closest to fp within
// maxDistance, ignoring excludeID. ok is false when nothing qualifies.
func (r *SampleRepo) nearestFingerprintID(ctx context.Context, fp string, maxDistance int, excludeID int64) (int64, bool, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, fingerprint FROM samples WHERE fingerprint<>''`)
	if err != nil {
		return 0, false, err
	}
	defer rows.Close()

	bestID := int64(0)
	bestDist := maxDistance + 1
	for rows.Next() {
		var id int64
		var cand string
		if err := rows.Scan(&id, &cand); err != nil {
			return 0, false, err
		}
		if id == excludeID {
			continue
		}
		d := audio.HammingHex(fp, cand)
		if d < 0 {
			continue
		}
		if d < bestDist {
			bestDist = d
			bestID = id
		}
	}
	if err := rows.Err(); err != nil {
		return 0, false, err
	}
	if bestDist <= maxDistance {
		return bestID, true, nil
	}
	return 0, false, nil
}

// Similar returns up to limit samples ranked by fingerprint proximity to id.
// It searches within the same category first; if fewer than limit results are
// found under the acoustic threshold, it widens the search to all categories.
func (r *SampleRepo) Similar(ctx context.Context, id int64, limit int) ([]*domain.Sample, error) {
	base, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if base.Fingerprint == "" {
		return []*domain.Sample{}, nil
	}
	if limit <= 0 {
		limit = 20
	}

	type scored struct {
		id   int64
		dist int
	}

	scanRows := func(query string, args ...any) ([]scored, error) {
		rows, err := r.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		var out []scored
		for rows.Next() {
			var sid int64
			var fp string
			if err := rows.Scan(&sid, &fp); err != nil {
				rows.Close()
				return nil, err
			}
			d := audio.HammingHex(base.Fingerprint, fp)
			if d >= 0 {
				out = append(out, scored{id: sid, dist: d})
			}
		}
		rows.Close()
		return out, rows.Err()
	}

	// Phase 1: same-category scan (fast — typically small subset).
	all, err := scanRows(
		`SELECT id, fingerprint FROM samples WHERE fingerprint<>'' AND id<>? AND category=?`,
		id, string(base.Category),
	)
	if err != nil {
		return nil, err
	}

	// Phase 2: if not enough matches, search remaining categories too.
	if len(all) < limit {
		rest, err := scanRows(
			`SELECT id, fingerprint FROM samples WHERE fingerprint<>'' AND id<>? AND category<>?`,
			id, string(base.Category),
		)
		if err != nil {
			return nil, err
		}
		all = append(all, rest...)
	}

	sort.Slice(all, func(i, j int) bool { return all[i].dist < all[j].dist })
	if len(all) > limit {
		all = all[:limit]
	}
	out := make([]*domain.Sample, 0, len(all))
	for _, sc := range all {
		s, err := r.GetByID(ctx, sc.id)
		if err != nil {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// Search runs a filtered, sorted, paginated query and returns the page plus the
// total match count (for pagination UI).
func (r *SampleRepo) Search(ctx context.Context, q domain.SearchQuery) ([]*domain.Sample, int, error) {
	where, args := buildSampleWhere(q)
	from := "FROM samples"
	if strings.TrimSpace(q.Text) != "" {
		from += " JOIN samples_fts ON samples_fts.rowid = samples.id"
	}

	// Total count first (same filters, no order/limit).
	countSQL := "SELECT COUNT(*) " + from
	if where != "" {
		countSQL += " WHERE " + where
	}
	var total int
	if err := r.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Page query.
	pageSQL := "SELECT " + qualify(sampleColumns) + " " + from
	if where != "" {
		pageSQL += " WHERE " + where
	}
	pageSQL += " ORDER BY " + sampleOrderBy(q)
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	pageSQL += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, max0(q.Offset))

	rows, err := r.db.QueryContext(ctx, pageSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*domain.Sample
	for rows.Next() {
		s, err := scanSample(rows.Scan)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if out == nil {
		out = []*domain.Sample{}
	}
	return out, total, nil
}

// buildSampleWhere assembles the WHERE clause and argument list from a query.
func buildSampleWhere(q domain.SearchQuery) (string, []any) {
	var clauses []string
	var args []any

	if t := strings.TrimSpace(q.Text); t != "" {
		clauses = append(clauses, "samples_fts MATCH ?")
		args = append(args, ftsQuery(t))
	}
	if len(q.Categories) > 0 {
		ph := make([]string, len(q.Categories))
		for i, c := range q.Categories {
			ph[i] = "?"
			args = append(args, string(c))
		}
		clauses = append(clauses, "samples.category IN ("+strings.Join(ph, ",")+")")
	}
	if len(q.Origins) > 0 {
		ph := make([]string, len(q.Origins))
		for i, o := range q.Origins {
			ph[i] = "?"
			args = append(args, string(o))
		}
		clauses = append(clauses, "samples.origin IN ("+strings.Join(ph, ",")+")")
	}
	if len(q.Tags) > 0 {
		ph := make([]string, len(q.Tags))
		for i, tg := range q.Tags {
			ph[i] = "?"
			args = append(args, tg)
		}
		clauses = append(clauses,
			"samples.id IN (SELECT sample_id FROM sample_tags WHERE tag IN ("+
				strings.Join(ph, ",")+") GROUP BY sample_id HAVING COUNT(DISTINCT tag)=?)")
		args = append(args, len(q.Tags))
	}
	if q.MinBPM > 0 {
		clauses = append(clauses, "samples.bpm >= ?")
		args = append(args, q.MinBPM)
	}
	if q.MaxBPM > 0 {
		clauses = append(clauses, "samples.bpm <= ?")
		args = append(args, q.MaxBPM)
	}
	if q.MinSize > 0 {
		clauses = append(clauses, "samples.size >= ?")
		args = append(args, q.MinSize)
	}
	if q.MaxSize > 0 {
		clauses = append(clauses, "samples.size <= ?")
		args = append(args, q.MaxSize)
	}
	if q.FavOnly {
		clauses = append(clauses, "samples.favorite = 1")
	}
	if q.MinRating > 0 {
		clauses = append(clauses, "samples.rating >= ?")
		args = append(args, q.MinRating)
	}
	return strings.Join(clauses, " AND "), args
}

// sampleOrderBy maps the sort/order fields to a safe ORDER BY expression.
func sampleOrderBy(q domain.SearchQuery) string {
	col := "samples.added_at"
	switch strings.ToLower(q.Sort) {
	case "name":
		col = "samples.name COLLATE NOCASE"
	case "size":
		col = "samples.size"
	case "used":
		col = "samples.used_count"
	case "bpm":
		col = "samples.bpm"
	case "added", "":
		col = "samples.added_at"
	}
	dir := "DESC"
	if strings.EqualFold(q.Order, "asc") {
		dir = "ASC"
	}
	return col + " " + dir + ", samples.id " + dir
}

// SetCategory overwrites the sample's category (manual override).
func (r *SampleRepo) SetCategory(ctx context.Context, id int64, cat string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE samples SET category=?, auto=0, modified_at=? WHERE id=?`,
		cat, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	// Refresh FTS category column.
	var name, source, tags string
	if err := tx.QueryRowContext(ctx,
		`SELECT name, source_label, tags_json FROM samples WHERE id=?`, id).
		Scan(&name, &source, &tags); err != nil {
		return err
	}
	var tagList []string
	if tags != "" {
		_ = json.Unmarshal([]byte(tags), &tagList)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM samples_fts WHERE rowid=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO samples_fts(rowid, name, tags, category, source) VALUES(?,?,?,?,?)`,
		id, name, strings.Join(tagList, " "), cat, source); err != nil {
		return err
	}
	return tx.Commit()
}

// SetFavorite toggles the favorite flag.
func (r *SampleRepo) SetFavorite(ctx context.Context, id int64, fav bool) error {
	return r.touch(ctx, `UPDATE samples SET favorite=?, modified_at=? WHERE id=?`, boolToInt(fav), time.Now().Unix(), id)
}

// SetRating sets a 0..5 rating.
func (r *SampleRepo) SetRating(ctx context.Context, id int64, rating int) error {
	if rating < 0 {
		rating = 0
	}
	if rating > 5 {
		rating = 5
	}
	return r.touch(ctx, `UPDATE samples SET rating=?, modified_at=? WHERE id=?`, rating, time.Now().Unix(), id)
}

// SetTags replaces a sample's tags (row JSON, normalised rows, and FTS).
func (r *SampleRepo) SetTags(ctx context.Context, id int64, tags []string) error {
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, _ := json.Marshal(tags)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `UPDATE samples SET tags_json=?, modified_at=? WHERE id=?`,
		string(tagsJSON), time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	if err := syncSampleTags(ctx, tx, id, tags); err != nil {
		return err
	}
	// Refresh the FTS tags column without disturbing the rest of the row.
	var name, category, source string
	if err := tx.QueryRowContext(ctx, `SELECT name, category, source_label FROM samples WHERE id=?`, id).
		Scan(&name, &category, &source); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM samples_fts WHERE rowid=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO samples_fts(rowid, name, tags, category, source) VALUES(?,?,?,?,?)`,
		id, name, strings.Join(tags, " "), category, source); err != nil {
		return err
	}
	return tx.Commit()
}

// IncrementUsed adjusts the usage counter (delta may be negative).
func (r *SampleRepo) IncrementUsed(ctx context.Context, id int64, delta int) error {
	return r.touch(ctx, `UPDATE samples SET used_count = MAX(0, used_count + ?), modified_at=? WHERE id=?`,
		delta, time.Now().Unix(), id)
}

// Rename updates a sample's display name and on-disk path together, refreshing
// the name in the full-text index.
func (r *SampleRepo) Rename(ctx context.Context, id int64, newName, newPath string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE samples SET name=?, path=?, modified_at=? WHERE id=?`,
		newName, newPath, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	var category, source, tags string
	if err := tx.QueryRowContext(ctx,
		`SELECT category, source_label, tags_json FROM samples WHERE id=?`, id).
		Scan(&category, &source, &tags); err != nil {
		return err
	}
	var tagList []string
	if tags != "" {
		_ = json.Unmarshal([]byte(tags), &tagList)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM samples_fts WHERE rowid=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO samples_fts(rowid, name, tags, category, source) VALUES(?,?,?,?,?)`,
		id, newName, strings.Join(tagList, " "), category, source); err != nil {
		return err
	}
	return tx.Commit()
}

// Delete removes a sample and its dependent rows (cascade + FTS).
func (r *SampleRepo) Delete(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM samples_fts WHERE rowid=?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM samples WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return tx.Commit()
}

// DeleteAll removes every sample and clears the FTS index.
func (r *SampleRepo) DeleteAll(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM samples_fts`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM samples`); err != nil {
		return err
	}
	return tx.Commit()
}

// Count returns the total number of samples.
func (r *SampleRepo) Count(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM samples`).Scan(&n)
	return n, err
}

// touch runs an UPDATE and maps "no rows" to ErrNotFound.
func (r *SampleRepo) touch(ctx context.Context, q string, args ...any) error {
	res, err := r.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// --- small helpers ---

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// qualify prefixes bare sample columns with the table name for JOIN queries.
func qualify(cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = "samples." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

// ftsQuery turns free user text into a safe FTS5 prefix query. Each whitespace
// token is quoted (defeating FTS operator injection) and given a prefix match,
// so "dark 808" matches "darker" and "808s".
func ftsQuery(text string) string {
	fields := strings.Fields(text)
	quoted := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.ReplaceAll(f, `"`, `""`)
		quoted = append(quoted, `"`+f+`"*`)
	}
	return strings.Join(quoted, " ")
}
