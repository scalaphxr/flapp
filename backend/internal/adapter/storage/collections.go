package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/flapp/core/internal/domain"
)

// CollectionRepo implements domain.CollectionRepository over SQLite.
type CollectionRepo struct {
	db *sql.DB
}

// Create inserts a new collection and its initial members.
func (r *CollectionRepo) Create(ctx context.Context, c *domain.Collection) (int64, error) {
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO collections(name, note, created_at) VALUES(?,?,?)`,
		c.Name, c.Note, c.CreatedAt.Unix())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	c.ID = id
	if err := addCollectionSamples(ctx, tx, id, c.SampleIDs); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// GetByID returns a collection with its member sample ids.
func (r *CollectionRepo) GetByID(ctx context.Context, id int64) (*domain.Collection, error) {
	var (
		c         domain.Collection
		createdAt int64
	)
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, note, created_at FROM collections WHERE id=?`, id).
		Scan(&c.ID, &c.Name, &c.Note, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.CreatedAt = time.Unix(createdAt, 0)

	ids, err := r.memberIDs(ctx, id)
	if err != nil {
		return nil, err
	}
	c.SampleIDs = ids
	return &c, nil
}

// List returns all collections (with members) newest first.
func (r *CollectionRepo) List(ctx context.Context) ([]*domain.Collection, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, note, created_at FROM collections ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []*domain.Collection
	for rows.Next() {
		var (
			c         domain.Collection
			createdAt int64
		)
		if err := rows.Scan(&c.ID, &c.Name, &c.Note, &createdAt); err != nil {
			return nil, err
		}
		c.CreatedAt = time.Unix(createdAt, 0)
		cols = append(cols, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, c := range cols {
		ids, err := r.memberIDs(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		c.SampleIDs = ids
	}
	if cols == nil {
		cols = []*domain.Collection{}
	}
	return cols, nil
}

// AddSamples appends samples to a collection (idempotent).
func (r *CollectionRepo) AddSamples(ctx context.Context, id int64, sampleIDs []int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := addCollectionSamples(ctx, tx, id, sampleIDs); err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveSample detaches a single sample from a collection.
func (r *CollectionRepo) RemoveSample(ctx context.Context, id int64, sampleID int64) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM collection_samples WHERE collection_id=? AND sample_id=?`, id, sampleID)
	return err
}

// Delete removes a collection (membership rows cascade).
func (r *CollectionRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM collections WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *CollectionRepo) memberIDs(ctx context.Context, id int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT sample_id FROM collection_samples WHERE collection_id=? ORDER BY sample_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var sid int64
		if err := rows.Scan(&sid); err != nil {
			return nil, err
		}
		ids = append(ids, sid)
	}
	return ids, rows.Err()
}

func addCollectionSamples(ctx context.Context, tx *sql.Tx, id int64, sampleIDs []int64) error {
	if len(sampleIDs) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO collection_samples(collection_id, sample_id) VALUES(?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, sid := range sampleIDs {
		if _, err := stmt.ExecContext(ctx, id, sid); err != nil {
			return err
		}
	}
	return nil
}
