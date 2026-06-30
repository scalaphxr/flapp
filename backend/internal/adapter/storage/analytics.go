package storage

import (
	"context"
	"database/sql"

	"github.com/flapp/core/internal/domain"
)

// AnalyticsRepo implements domain.AnalyticsRepository over SQLite.
type AnalyticsRepo struct {
	db *sql.DB
}

// Overview computes the dashboard aggregates in a handful of grouped queries.
func (r *AnalyticsRepo) Overview(ctx context.Context) (*domain.Analytics, error) {
	a := &domain.Analytics{
		ByCategory: []domain.CategoryCount{},
		TopUsed:    []domain.SampleRef{},
		TopBPM:     []domain.BPMCount{},
		TopKeys:    []domain.KeyCount{},
		TopTags:    []domain.TagCount{},
	}

	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM projects`).Scan(&a.Projects); err != nil {
		return nil, err
	}
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(size),0) FROM samples`).Scan(&a.Samples, &a.BytesTotal); err != nil {
		return nil, err
	}
	// Every stored sample is unique by construction (dedup happens before
	// insert), so unique == count and duplicates are reported per harvest run.
	a.UniqueSamples = a.Samples

	if err := r.categoryBreakdown(ctx, a); err != nil {
		return nil, err
	}
	if err := r.topUsed(ctx, a); err != nil {
		return nil, err
	}
	if err := r.topBPM(ctx, a); err != nil {
		return nil, err
	}
	if err := r.topKeys(ctx, a); err != nil {
		return nil, err
	}
	if err := r.topTags(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (r *AnalyticsRepo) categoryBreakdown(ctx context.Context, a *domain.Analytics) error {
	rows, err := r.db.QueryContext(ctx,
		`SELECT category, COUNT(*), COALESCE(SUM(size),0) FROM samples GROUP BY category ORDER BY COUNT(*) DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cc domain.CategoryCount
		if err := rows.Scan(&cc.Category, &cc.Count, &cc.Bytes); err != nil {
			return err
		}
		cc.ColorGroup = cc.Category.Group()
		a.ByCategory = append(a.ByCategory, cc)
	}
	return rows.Err()
}

func (r *AnalyticsRepo) topUsed(ctx context.Context, a *domain.Analytics) error {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, used_count, category FROM samples WHERE used_count > 0
		 ORDER BY used_count DESC, id DESC LIMIT 20`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sr domain.SampleRef
		if err := rows.Scan(&sr.ID, &sr.Name, &sr.Used, &sr.Cat); err != nil {
			return err
		}
		a.TopUsed = append(a.TopUsed, sr)
	}
	return rows.Err()
}

func (r *AnalyticsRepo) topBPM(ctx context.Context, a *domain.Analytics) error {
	rows, err := r.db.QueryContext(ctx,
		`SELECT bpm, COUNT(*) FROM samples WHERE bpm > 0 GROUP BY bpm ORDER BY COUNT(*) DESC LIMIT 12`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var bc domain.BPMCount
		if err := rows.Scan(&bc.BPM, &bc.Count); err != nil {
			return err
		}
		a.TopBPM = append(a.TopBPM, bc)
	}
	return rows.Err()
}

func (r *AnalyticsRepo) topKeys(ctx context.Context, a *domain.Analytics) error {
	rows, err := r.db.QueryContext(ctx,
		`SELECT key_name, COUNT(*) FROM samples WHERE key_name <> '' GROUP BY key_name ORDER BY COUNT(*) DESC LIMIT 12`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var kc domain.KeyCount
		if err := rows.Scan(&kc.Key, &kc.Count); err != nil {
			return err
		}
		a.TopKeys = append(a.TopKeys, kc)
	}
	return rows.Err()
}

func (r *AnalyticsRepo) topTags(ctx context.Context, a *domain.Analytics) error {
	rows, err := r.db.QueryContext(ctx,
		`SELECT tag, COUNT(*) FROM sample_tags GROUP BY tag ORDER BY COUNT(*) DESC LIMIT 30`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tc domain.TagCount
		if err := rows.Scan(&tc.Tag, &tc.Count); err != nil {
			return err
		}
		a.TopTags = append(a.TopTags, tc)
	}
	return rows.Err()
}

// TagRepo implements domain.TagRepository over SQLite.
type TagRepo struct {
	db *sql.DB
}

// AllTags returns the global tag vocabulary with usage counts.
func (r *TagRepo) AllTags(ctx context.Context) ([]domain.TagCount, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT tag, COUNT(*) FROM sample_tags GROUP BY tag ORDER BY COUNT(*) DESC, tag ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.TagCount{}
	for rows.Next() {
		var tc domain.TagCount
		if err := rows.Scan(&tc.Tag, &tc.Count); err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}
