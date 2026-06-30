package domain

import (
	"context"
	"errors"
)

// Common domain errors.
var (
	ErrNotFound     = errors.New("not found")
	ErrUnsupported  = errors.New("unsupported format")
	ErrCanceled     = errors.New("operation canceled")
	ErrInvalidInput = errors.New("invalid input")
)

// SampleRepository is the persistence port for samples (implemented by SQLite).
type SampleRepository interface {
	Upsert(ctx context.Context, s *Sample) (int64, error)
	GetByID(ctx context.Context, id int64) (*Sample, error)
	FindByHash(ctx context.Context, md5, sha256 string) (*Sample, error)
	FindByFingerprint(ctx context.Context, fp string, maxDistance int) (*Sample, error)
	Search(ctx context.Context, q SearchQuery) ([]*Sample, int, error)
	Similar(ctx context.Context, id int64, limit int) ([]*Sample, error)
	SetCategory(ctx context.Context, id int64, cat string) error
	SetFavorite(ctx context.Context, id int64, fav bool) error
	SetRating(ctx context.Context, id int64, rating int) error
	SetTags(ctx context.Context, id int64, tags []string) error
	IncrementUsed(ctx context.Context, id int64, delta int) error
	Rename(ctx context.Context, id int64, newName, newPath string) error
	Delete(ctx context.Context, id int64) error
	DeleteAll(ctx context.Context) error
	Count(ctx context.Context) (int, error)
	// Peaks cache — хранит пики в БД, чтобы не декодировать файл на каждый запрос.
	GetPeaksJSON(ctx context.Context, id int64) (string, error)
	SetPeaksJSON(ctx context.Context, id int64, json string) error
	// Peaks v2 cache — пары [min, max] вместо одиночных амплитуд (лучшая форма волны).
	GetPeaks2JSON(ctx context.Context, id int64) (string, error)
	SetPeaks2JSON(ctx context.Context, id int64, json string) error
}

// ProjectRepository persists parsed FLP projects.
type ProjectRepository interface {
	Upsert(ctx context.Context, p *Project) (int64, error)
	GetByID(ctx context.Context, id int64) (*Project, error)
	Search(ctx context.Context, text string, limit, offset int) ([]*Project, int, error)
	Delete(ctx context.Context, id int64) error
	Count(ctx context.Context) (int, error)
}

// CollectionRepository persists user collections.
type CollectionRepository interface {
	Create(ctx context.Context, c *Collection) (int64, error)
	GetByID(ctx context.Context, id int64) (*Collection, error)
	List(ctx context.Context) ([]*Collection, error)
	AddSamples(ctx context.Context, id int64, sampleIDs []int64) error
	RemoveSample(ctx context.Context, id int64, sampleID int64) error
	Delete(ctx context.Context, id int64) error
}

// AnalyticsRepository serves aggregated statistics.
type AnalyticsRepository interface {
	Overview(ctx context.Context) (*Analytics, error)
}

// TagRepository serves the global tag vocabulary.
type TagRepository interface {
	AllTags(ctx context.Context) ([]TagCount, error)
}

// --- service ports (infrastructure capabilities) ---

// ArchiveExtractor walks an archive and yields each contained file.
type ArchiveExtractor interface {
	// Supports reports whether the extension is handled (zip/rar/7z).
	Supports(ext string) bool
	// Extract streams entries to fn; fn receives a temp path it may move.
	Extract(ctx context.Context, archivePath, destDir string, fn func(entry ExtractedFile) error) error
}

// ExtractedFile is one file produced by an extractor.
type ExtractedFile struct {
	Name     string // base name
	RelPath  string // path inside the archive
	TempPath string // where it was written on disk
	Size     int64
}

// FLPParser parses a .flp file into a Project.
type FLPParser interface {
	Parse(ctx context.Context, path string) (*Project, error)
	// ParseBytes парсит FLP из уже загруженного байт-буфера.
	// displayName — имя для отображения; srcPath — путь для library bookkeeping.
	ParseBytes(ctx context.Context, raw []byte, displayName, srcPath string) (*Project, error)
}

// AudioAnalyzer extracts metadata and signal features from an audio file.
type AudioAnalyzer interface {
	Analyze(ctx context.Context, path string) (AudioFeatures, error)
	// Fingerprint returns a perceptual hash usable for acoustic similarity.
	Fingerprint(ctx context.Context, path string) (string, error)
	// AnalyzeAll decodes the file once and returns both features and fingerprint.
	// Prefer this over separate Analyze + Fingerprint calls on the same path.
	AnalyzeAll(ctx context.Context, path string) (AudioFeatures, string, error)
}

// Classifier assigns a Category to a sample.
type Classifier interface {
	// Classify uses name/folder hints first, then audio features.
	Classify(name, relPath string, f AudioFeatures) (cat Category, fromAudio bool)
}

// TagGenerator derives descriptive tags from a sample.
type TagGenerator interface {
	Generate(s *Sample) []string
}

// Hasher computes content hashes.
type Hasher interface {
	Hashes(path string) (md5hex, sha256hex string, err error)
}

// Packer writes a set of files into an archive (zip/7z) preserving structure.
type Packer interface {
	Pack(ctx context.Context, dest string, format string, entries []PackEntry, progress func(done, total int)) error
}

// PackEntry is one file destined for an exported pack.
type PackEntry struct {
	SourcePath string
	ArcPath    string // path inside the produced archive
}

// JobQueue runs background work and exposes progress.
type JobQueue interface {
	Enqueue(t JobType, run func(ctx context.Context, report ProgressReporter) (map[string]interface{}, error)) string
	Get(id string) (*Job, bool)
	List() []*Job
	Cancel(id string) bool
	Subscribe() (<-chan *Job, func())
}

// ProgressReporter is handed to a running job to publish progress.
type ProgressReporter interface {
	Set(progress float64, stage, detail string)
	Stage(stage string)
	Detail(detail string)
}

// Analytics is the aggregated dashboard payload.
type Analytics struct {
	Projects      int             `json:"projects"`
	Samples       int             `json:"samples"`
	UniqueSamples int             `json:"uniqueSamples"`
	Duplicates    int             `json:"duplicates"`
	BytesTotal    int64           `json:"bytesTotal"`
	BytesSaved    int64           `json:"bytesSaved"`
	ByCategory    []CategoryCount `json:"byCategory"`
	TopUsed       []SampleRef     `json:"topUsed"`
	TopBPM        []BPMCount      `json:"topBpm"`
	TopKeys       []KeyCount      `json:"topKeys"`
	TopTags       []TagCount      `json:"topTags"`
}

type CategoryCount struct {
	Category   Category   `json:"category"`
	ColorGroup ColorGroup `json:"colorGroup"`
	Count      int        `json:"count"`
	Bytes      int64      `json:"bytes"`
}
type SampleRef struct {
	ID   int64    `json:"id"`
	Name string   `json:"name"`
	Used int      `json:"used"`
	Cat  Category `json:"category"`
}
type BPMCount struct {
	BPM   int `json:"bpm"`
	Count int `json:"count"`
}
type KeyCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}
