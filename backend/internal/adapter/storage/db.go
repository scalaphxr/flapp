package storage

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"
)

// Store owns the database handle and exposes the repository implementations.
// All repositories share the single connection pool.
type Store struct {
	db *sql.DB

	Samples        *SampleRepo
	Projects       *ProjectRepo
	Collections    *CollectionRepo
	Analytics      *AnalyticsRepo
	Tags           *TagRepo
	AnalysisCache  *AnalysisCacheRepo
	HarvestIndex   *HarvestIndexRepo
	Checkpoint     *CheckpointRepo
	Unresolved     *UnresolvedRepo
}

// Open opens (creating if needed) the SQLite database at path, applies the
// schema, and returns a ready Store. WAL mode plus a busy timeout keep readers
// and the single writer from tripping over each other; the connection pool is
// capped at one because SQLite serialises writes anyway and this removes all
// "database is locked" races in the desktop single-process setting.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)&_pragma=synchronous(NORMAL)",
		url.PathEscape(path),
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if _, err := db.Exec(migrationSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply migration: %w", err)
	}
	if _, err := db.Exec(schemaV2SQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema v2: %w", err)
	}

	// Column additions for existing databases — ALTER TABLE is idempotent
	// because SQLite does not support IF NOT EXISTS for columns; we ignore
	// the "duplicate column name" error that fires on already-migrated DBs.
	for _, alter := range []string{
		`ALTER TABLE projects ADD COLUMN time_spent_s INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE samples ADD COLUMN peaks_json TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE samples ADD COLUMN spectral_flatness REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE samples ADD COLUMN crest_factor REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE samples ADD COLUMN decay_rate REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE samples ADD COLUMN onset_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE samples ADD COLUMN sub_bass_ratio REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE samples ADD COLUMN peaks2_json TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := db.Exec(alter); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			db.Close()
			return nil, fmt.Errorf("column migration: %w", err)
		}
	}

	s := &Store{db: db}
	s.Samples = &SampleRepo{db: db}
	s.Projects = &ProjectRepo{db: db}
	s.Collections = &CollectionRepo{db: db}
	s.Analytics = &AnalyticsRepo{db: db}
	s.Tags = &TagRepo{db: db}
	s.AnalysisCache = &AnalysisCacheRepo{db: db}
	s.HarvestIndex = &HarvestIndexRepo{db: db}
	s.Checkpoint = &CheckpointRepo{db: db}
	s.Unresolved = &UnresolvedRepo{db: db}
	return s, nil
}

// DB exposes the underlying handle (used by tests and maintenance tasks).
func (s *Store) DB() *sql.DB { return s.db }

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }
