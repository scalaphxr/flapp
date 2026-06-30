package storage

// schemaSQL is the full database schema. It is idempotent (IF NOT EXISTS) so it
// doubles as the migration for a fresh database and a no-op on an existing one.
//
// Design notes:
//   - samples.path carries a UNIQUE constraint and is the natural upsert key:
//     re-running a harvest over the same stored copies updates rather than
//     duplicates rows.
//   - Tags are stored both denormalised on the row (tags_json, for cheap single
//     row reads) and normalised in sample_tags (for filtering and aggregation).
//     Both are written inside one transaction so they never drift.
//   - samples_fts is a standalone FTS5 table (not external-content) keyed by
//     rowid = samples.id, giving fast full-text search over name/tags/category.
// migrationSQL runs after the schema to remap legacy fine-grained category
// names to the simplified 11-category taxonomy. Each statement is idempotent.
const migrationSQL = `
UPDATE samples SET category='Open Hat'  WHERE category IN ('Crash','Ride','Cymbal');
UPDATE samples SET category='Perc'      WHERE category IN ('Rim','Tom','Foley','One Shot');
UPDATE samples SET category='Vox'       WHERE category IN ('Chant','Shout','Vocal');
UPDATE samples SET category='FX'        WHERE category IN ('Sweep','Impact','Riser','Downlifter','MIDI','Texture','Ambience','Synth','Other');
UPDATE samples SET category='Loop'      WHERE category IN ('Piano','Guitar','Bell','Pluck','Pad','Bass','Melody','Melody Loop');
UPDATE samples SET category='Drum Loop' WHERE category='Fill';
`

const schemaSQL = `
CREATE TABLE IF NOT EXISTS samples (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT    NOT NULL,
    path         TEXT    NOT NULL UNIQUE,
    ext          TEXT    NOT NULL DEFAULT '',
    size         INTEGER NOT NULL DEFAULT 0,
    category     TEXT    NOT NULL DEFAULT 'Other',
    auto         INTEGER NOT NULL DEFAULT 0,
    origin       TEXT    NOT NULL DEFAULT 'folder',
    source_label TEXT    NOT NULL DEFAULT '',
    source_path  TEXT    NOT NULL DEFAULT '',
    md5          TEXT    NOT NULL DEFAULT '',
    sha256       TEXT    NOT NULL DEFAULT '',
    fingerprint  TEXT    NOT NULL DEFAULT '',
    bpm          INTEGER NOT NULL DEFAULT 0,
    key_name     TEXT    NOT NULL DEFAULT '',
    favorite     INTEGER NOT NULL DEFAULT 0,
    rating       INTEGER NOT NULL DEFAULT 0,
    used_count   INTEGER NOT NULL DEFAULT 0,
    tags_json    TEXT    NOT NULL DEFAULT '[]',
    added_at     INTEGER NOT NULL DEFAULT 0,
    modified_at  INTEGER NOT NULL DEFAULT 0,

    sample_rate  INTEGER NOT NULL DEFAULT 0,
    channels     INTEGER NOT NULL DEFAULT 0,
    bit_depth    INTEGER NOT NULL DEFAULT 0,
    duration     REAL    NOT NULL DEFAULT 0,
    rms          REAL    NOT NULL DEFAULT 0,
    peak         REAL    NOT NULL DEFAULT 0,
    centroid     REAL    NOT NULL DEFAULT 0,
    zcr          REAL    NOT NULL DEFAULT 0,
    low_ratio    REAL    NOT NULL DEFAULT 0,
    high_ratio   REAL    NOT NULL DEFAULT 0,
    attack       REAL    NOT NULL DEFAULT 0,
    analyzed     INTEGER NOT NULL DEFAULT 0,
    peaks_json   TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_samples_sha      ON samples(sha256);
CREATE INDEX IF NOT EXISTS idx_samples_md5      ON samples(md5);
CREATE INDEX IF NOT EXISTS idx_samples_category ON samples(category);
CREATE INDEX IF NOT EXISTS idx_samples_bpm      ON samples(bpm);
CREATE INDEX IF NOT EXISTS idx_samples_used     ON samples(used_count);
CREATE INDEX IF NOT EXISTS idx_samples_added    ON samples(added_at);

CREATE TABLE IF NOT EXISTS sample_tags (
    sample_id INTEGER NOT NULL REFERENCES samples(id) ON DELETE CASCADE,
    tag       TEXT    NOT NULL,
    PRIMARY KEY (sample_id, tag)
);
CREATE INDEX IF NOT EXISTS idx_sample_tags_tag ON sample_tags(tag);

CREATE VIRTUAL TABLE IF NOT EXISTS samples_fts USING fts5(
    name, tags, category, source
);

CREATE TABLE IF NOT EXISTS projects (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT    NOT NULL,
    path          TEXT    NOT NULL UNIQUE,
    title         TEXT    NOT NULL DEFAULT '',
    artist        TEXT    NOT NULL DEFAULT '',
    bpm           REAL    NOT NULL DEFAULT 0,
    key_name      TEXT    NOT NULL DEFAULT '',
    flp_version   TEXT    NOT NULL DEFAULT '',
    size          INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL DEFAULT 0,
    added_at      INTEGER NOT NULL DEFAULT 0,
    tags_json     TEXT    NOT NULL DEFAULT '[]',
    samples_json  TEXT    NOT NULL DEFAULT '[]',
    plugins_json  TEXT    NOT NULL DEFAULT '[]',
    channels_json TEXT    NOT NULL DEFAULT '[]',
    time_spent_s  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_projects_bpm ON projects(bpm);

CREATE VIRTUAL TABLE IF NOT EXISTS projects_fts USING fts5(
    name, title, artist, plugins
);

CREATE TABLE IF NOT EXISTS collections (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    note       TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS collection_samples (
    collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    sample_id     INTEGER NOT NULL REFERENCES samples(id) ON DELETE CASCADE,
    PRIMARY KEY (collection_id, sample_id)
);
`
