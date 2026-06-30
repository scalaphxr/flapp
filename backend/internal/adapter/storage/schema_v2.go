package storage

// schemaV2SQL adds the tables required by the two-phase analysis pipeline.
// All statements are idempotent (IF NOT EXISTS / ignore duplicate column) so
// they can run on both fresh and existing databases without version tracking.
const schemaV2SQL = `
-- -------------------------------------------------------------------------
-- Phase-1 fast index: records every discovered file with quick-hash and
-- detected format. Status tracks pipeline progress per file.
-- -------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS harvest_index (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    path         TEXT    NOT NULL,
    mtime        INTEGER NOT NULL DEFAULT 0,
    size         INTEGER NOT NULL DEFAULT 0,
    quick_hash   TEXT    NOT NULL DEFAULT '',
    content_hash TEXT    NOT NULL DEFAULT '',
    fmt          TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL DEFAULT 'new',
    error_msg    TEXT    NOT NULL DEFAULT '',
    indexed_at   INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_hi_path  ON harvest_index(path);
CREATE INDEX        IF NOT EXISTS idx_hi_qhash ON harvest_index(quick_hash);
CREATE INDEX        IF NOT EXISTS idx_hi_chash ON harvest_index(content_hash);
CREATE INDEX        IF NOT EXISTS idx_hi_status ON harvest_index(status);

-- -------------------------------------------------------------------------
-- Content-addressed analysis cache.
-- Keyed by SHA-256: if we have seen these exact bytes before, skip re-analysis.
-- -------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS analysis_cache (
    content_hash  TEXT    NOT NULL PRIMARY KEY,
    features_json TEXT    NOT NULL DEFAULT '{}',
    fingerprint   TEXT    NOT NULL DEFAULT '',
    analyzed_at   INTEGER NOT NULL DEFAULT 0
);

-- -------------------------------------------------------------------------
-- Extended asset table (normalised counterpart to samples).
-- Allows multiple sources to reference the same content.
-- -------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS assets (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    content_hash TEXT    NOT NULL,
    ext          TEXT    NOT NULL DEFAULT '',
    duration     REAL    NOT NULL DEFAULT 0,
    bpm          REAL    NOT NULL DEFAULT 0,
    key_name     TEXT    NOT NULL DEFAULT '',
    rms          REAL    NOT NULL DEFAULT 0,
    spectral     REAL    NOT NULL DEFAULT 0,
    phash        TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL DEFAULT 'new'
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_assets_chash ON assets(content_hash);
CREATE INDEX        IF NOT EXISTS idx_assets_phash ON assets(phash);

-- Source locations: archive entries, FLP internal refs, loose files.
CREATE TABLE IF NOT EXISTS sources (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    source_type       TEXT    NOT NULL DEFAULT 'file',
    path              TEXT    NOT NULL,
    archive_parent_id INTEGER REFERENCES sources(id) ON DELETE SET NULL,
    flp_project_id    INTEGER REFERENCES projects(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_sources_path ON sources(path);

-- Many-to-many: one asset can appear in many archives/projects.
CREATE TABLE IF NOT EXISTS asset_sources (
    asset_id    INTEGER NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    source_id   INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    origin_path TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (asset_id, source_id)
);
CREATE INDEX IF NOT EXISTS idx_asset_sources_src ON asset_sources(source_id);

-- -------------------------------------------------------------------------
-- FL Studio project extended metadata (richer than the projects table).
-- -------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS projects_flp (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id   INTEGER NOT NULL UNIQUE REFERENCES projects(id) ON DELETE CASCADE,
    tempo_map    TEXT    NOT NULL DEFAULT '[]',
    ppq          INTEGER NOT NULL DEFAULT 96,
    parse_status TEXT    NOT NULL DEFAULT 'ok'
);

-- Unresolved FLP sample path references (file not found on disk).
CREATE TABLE IF NOT EXISTS unresolved_assets (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id     INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    raw_path       TEXT    NOT NULL,
    normalized     TEXT    NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_unresolved_proj ON unresolved_assets(project_id);

-- -------------------------------------------------------------------------
-- MIDI exports table.
-- -------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS midi_exports (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    asset_id   INTEGER REFERENCES assets(id) ON DELETE SET NULL,
    mode       TEXT    NOT NULL DEFAULT 'strict',
    path       TEXT    NOT NULL,
    hash       TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_midi_exports_proj ON midi_exports(project_id);

-- -------------------------------------------------------------------------
-- Checkpoint table for large-archive batch processing.
-- Allows resume after crash / cancellation.
-- -------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS harvest_checkpoint (
    job_id       TEXT    NOT NULL,
    archive_path TEXT    NOT NULL,
    entry_name   TEXT    NOT NULL,
    entry_crc    INTEGER NOT NULL DEFAULT 0,
    entry_size   INTEGER NOT NULL DEFAULT 0,
    status       TEXT    NOT NULL DEFAULT 'pending',
    updated_at   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (job_id, archive_path, entry_name)
);
CREATE INDEX IF NOT EXISTS idx_checkpoint_job ON harvest_checkpoint(job_id, status);

-- -------------------------------------------------------------------------
-- LSH bucket table for perceptual near-dup at scale.
-- Each phash is split into 4 bands × 4 bytes → 16 short keys.
-- A sample matches another if at least one bucket overlaps.
-- -------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS phash_lsh_buckets (
    sample_id INTEGER NOT NULL REFERENCES samples(id) ON DELETE CASCADE,
    bucket    TEXT    NOT NULL,
    PRIMARY KEY (sample_id, bucket)
);
CREATE INDEX IF NOT EXISTS idx_phash_lsh ON phash_lsh_buckets(bucket);
`
