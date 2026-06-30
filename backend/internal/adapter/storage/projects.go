package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/flapp/core/internal/domain"
)

// ProjectRepo implements domain.ProjectRepository over SQLite.
type ProjectRepo struct {
	db *sql.DB
}

const projectColumns = `id, name, path, title, artist, bpm, key_name,
	flp_version, size, created_at, added_at, tags_json, samples_json,
	plugins_json, channels_json, time_spent_s`

func scanProject(scan func(dest ...any) error) (*domain.Project, error) {
	var (
		p            domain.Project
		createdAt    int64
		addedAt      int64
		tagsJSON     string
		samplesJSON  string
		pluginsJSON  string
		channelsJSON string
	)
	err := scan(
		&p.ID, &p.Name, &p.Path, &p.Title, &p.Artist, &p.BPM, &p.KeyName,
		&p.FLPVersion, &p.Size, &createdAt, &addedAt,
		&tagsJSON, &samplesJSON, &pluginsJSON, &channelsJSON, &p.TimeSpentSeconds,
	)
	if err != nil {
		return nil, err
	}
	p.CreatedAt = time.Unix(createdAt, 0)
	p.AddedAt = time.Unix(addedAt, 0)
	unmarshalInto(tagsJSON, &p.Tags)
	unmarshalInto(samplesJSON, &p.SamplePaths)
	unmarshalInto(pluginsJSON, &p.Plugins)
	unmarshalInto(channelsJSON, &p.Channels)
	if p.Tags == nil {
		p.Tags = []string{}
	}
	if p.SamplePaths == nil {
		p.SamplePaths = []string{}
	}
	if p.Plugins == nil {
		p.Plugins = []string{}
	}
	if p.Channels == nil {
		p.Channels = []domain.FLPChannel{}
	}
	return &p, nil
}

// Upsert inserts or updates a project keyed by its file path.
func (r *ProjectRepo) Upsert(ctx context.Context, p *domain.Project) (int64, error) {
	if p.AddedAt.IsZero() {
		p.AddedAt = time.Now()
	}
	tagsJSON, _ := json.Marshal(orEmptyStrings(p.Tags))
	samplesJSON, _ := json.Marshal(orEmptyStrings(p.SamplePaths))
	pluginsJSON, _ := json.Marshal(orEmptyStrings(p.Plugins))
	channelsJSON, _ := json.Marshal(p.Channels)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	const q = `
INSERT INTO projects (
    name, path, title, artist, bpm, key_name, flp_version, size,
    created_at, added_at, tags_json, samples_json, plugins_json, channels_json, time_spent_s
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(path) DO UPDATE SET
    name=excluded.name, title=excluded.title, artist=excluded.artist,
    bpm=excluded.bpm, key_name=excluded.key_name, flp_version=excluded.flp_version,
    size=excluded.size, created_at=excluded.created_at, tags_json=excluded.tags_json,
    samples_json=excluded.samples_json, plugins_json=excluded.plugins_json,
    channels_json=excluded.channels_json, time_spent_s=excluded.time_spent_s
RETURNING id`

	var id int64
	err = tx.QueryRowContext(ctx, q,
		p.Name, p.Path, p.Title, p.Artist, p.BPM, p.KeyName, p.FLPVersion, p.Size,
		p.CreatedAt.Unix(), p.AddedAt.Unix(),
		string(tagsJSON), string(samplesJSON), string(pluginsJSON), string(channelsJSON), p.TimeSpentSeconds,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	p.ID = id

	if _, err := tx.ExecContext(ctx, `DELETE FROM projects_fts WHERE rowid=?`, id); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO projects_fts(rowid, name, title, artist, plugins) VALUES(?,?,?,?,?)`,
		id, p.Name, p.Title, p.Artist, strings.Join(p.Plugins, " ")); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// GetByID fetches a project.
func (r *ProjectRepo) GetByID(ctx context.Context, id int64) (*domain.Project, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+projectColumns+` FROM projects WHERE id=?`, id)
	p, err := scanProject(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return p, err
}

// Search runs full-text search over projects (or lists all when text is empty).
func (r *ProjectRepo) Search(ctx context.Context, text string, limit, offset int) ([]*domain.Project, int, error) {
	if limit <= 0 {
		limit = 100
	}
	var (
		total int
		rows  *sql.Rows
		err   error
	)
	text = strings.TrimSpace(text)
	if text == "" {
		if err = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects`).Scan(&total); err != nil {
			return nil, 0, err
		}
		rows, err = r.db.QueryContext(ctx,
			`SELECT `+projectColumns+` FROM projects ORDER BY added_at DESC, id DESC LIMIT ? OFFSET ?`,
			limit, max0(offset))
	} else {
		match := ftsQuery(text)
		if err = r.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM projects JOIN projects_fts ON projects_fts.rowid=projects.id WHERE projects_fts MATCH ?`,
			match).Scan(&total); err != nil {
			return nil, 0, err
		}
		rows, err = r.db.QueryContext(ctx,
			`SELECT `+projectQualified()+` FROM projects JOIN projects_fts ON projects_fts.rowid=projects.id
			 WHERE projects_fts MATCH ? ORDER BY projects.added_at DESC, projects.id DESC LIMIT ? OFFSET ?`,
			match, limit, max0(offset))
	}
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*domain.Project
	for rows.Next() {
		p, err := scanProject(rows.Scan)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if out == nil {
		out = []*domain.Project{}
	}
	return out, total, nil
}

// Delete removes a project and its FTS row.
func (r *ProjectRepo) Delete(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM projects_fts WHERE rowid=?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return tx.Commit()
}

// Count returns the number of stored projects.
func (r *ProjectRepo) Count(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects`).Scan(&n)
	return n, err
}

func projectQualified() string {
	parts := strings.Split(projectColumns, ",")
	for i, p := range parts {
		parts[i] = "projects." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

func unmarshalInto(s string, v any) {
	if s == "" {
		return
	}
	_ = json.Unmarshal([]byte(s), v)
}

func orEmptyStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
