package domain

import "time"

// JobType enumerates the kinds of background work the engine performs.
type JobType string

const (
	JobHarvest      JobType = "harvest"       // scan inputs -> extract -> dedup -> store
	JobExportPack   JobType = "export_pack"   // build a sample pack / drum kit
	JobImportFolder JobType = "import_folder" // index an external library folder
	JobRename       JobType = "rename"        // batch rename files on disk
	JobReanalyze    JobType = "reanalyze"     // recompute features/fingerprints
	JobMidiExtract  JobType = "extract_midi"  // extract MIDI from FLP projects
)

// JobStatus is the lifecycle state of a job.
type JobStatus string

const (
	StatusQueued    JobStatus = "queued"
	StatusRunning   JobStatus = "running"
	StatusCompleted JobStatus = "completed"
	StatusFailed    JobStatus = "failed"
	StatusCanceled  JobStatus = "canceled"
)

// Job is a unit of background work with live progress.
type Job struct {
	ID        string                 `json:"id"`
	Type      JobType                `json:"type"`
	Status    JobStatus              `json:"status"`
	Progress  float64                `json:"progress"` // 0..1
	Stage     string                 `json:"stage"`    // human-readable current step
	Detail    string                 `json:"detail"`   // e.g. current filename
	Error     string                 `json:"error,omitempty"`
	Result    map[string]interface{} `json:"result,omitempty"`
	CreatedAt time.Time              `json:"createdAt"`
	UpdatedAt time.Time              `json:"updatedAt"`
}

// HarvestRequest configures a harvest run.
type HarvestRequest struct {
	Inputs       []string `json:"inputs"`       // files & folders dropped by the user
	DrumkitsDir  string   `json:"drumkitsDir"`  // resolve .flp sample paths against this
	Guess        bool     `json:"guess"`        // auto-detect type by audio analysis
	ExtraFormats bool     `json:"extraFormats"` // include flac/aiff/ogg/m4a
	DeepDedup    bool     `json:"deepDedup"`    // enable fingerprint/acoustic dedup
	OnlyFromFLP  bool     `json:"onlyFromFlp"`  // keep only samples referenced by projects
	GenerateTags bool     `json:"generateTags"` // auto tag on import
	// AcousticThreshold is the max Hamming distance for acoustic dedup. Zero
	// means "use the calibrated default".
	AcousticThreshold int `json:"acousticThreshold"`
}

// SearchQuery is the parameter object for library search & filtering.
type SearchQuery struct {
	Text       string     `json:"text"`
	Categories []Category `json:"categories"`
	Tags       []string   `json:"tags"`
	Origins    []Origin   `json:"origins"`
	MinBPM     int        `json:"minBpm"`
	MaxBPM     int        `json:"maxBpm"`
	MinSize    int64      `json:"minSize"`
	MaxSize    int64      `json:"maxSize"`
	FavOnly    bool       `json:"favOnly"`
	MinRating  int        `json:"minRating"`
	Sort       string     `json:"sort"`  // name|size|added|used|bpm
	Order      string     `json:"order"` // asc|desc
	Limit      int        `json:"limit"`
	Offset     int        `json:"offset"`
}
