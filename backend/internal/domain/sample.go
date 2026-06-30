package domain

import "time"

// Origin describes where a sample was discovered.
type Origin string

const (
	OriginArchive Origin = "archive" // came out of a .zip/.rar/.7z
	OriginProject Origin = "project" // referenced by an .flp project
	OriginFolder  Origin = "folder"  // loose file in a scanned folder
	OriginBoth    Origin = "both"    // present in an archive AND referenced by a project
)

// AudioFeatures holds the signal-level analysis used by the classifier and
// the acoustic-similarity engine. Zero values mean "not analysed".
type AudioFeatures struct {
	SampleRate      int     `json:"sampleRate"`
	Channels        int     `json:"channels"`
	BitDepth        int     `json:"bitDepth"`
	DurationSeconds float64 `json:"durationSeconds"`

	// Spectral / temporal descriptors (computed from decoded PCM for WAV/AIFF).
	RMS              float64 `json:"rms"`              // overall loudness
	PeakAmplitude    float64 `json:"peakAmplitude"`    // 0..1
	SpectralCentroid float64 `json:"spectralCentroid"` // Hz, "brightness"
	ZeroCrossRate    float64 `json:"zeroCrossRate"`    // noisiness proxy
	LowEnergyRatio   float64 `json:"lowEnergyRatio"`   // <150 Hz energy share
	HighEnergyRatio  float64 `json:"highEnergyRatio"`  // >6 kHz energy share
	AttackTime       float64 `json:"attackTime"`       // seconds to peak (transient)

	// Extended descriptors (v2; zero means not yet computed).
	SpectralFlatness float64 `json:"spectralFlatness"` // geometric/arithmetic mag ratio 0..1; 0=tonal, 1=noisy
	CrestFactor      float64 `json:"crestFactor"`      // peak/RMS; sharpness of transient
	DecayRate        float64 `json:"decayRate"`        // seconds from peak to 50% energy
	OnsetCount       int     `json:"onsetCount"`       // amplitude onset event count
	SubBassRatio     float64 `json:"subBassRatio"`     // spectral energy share below 80 Hz

	Analyzed bool `json:"analyzed"`
}

// Sample is the central entity: one unique audio file in the library.
type Sample struct {
	ID   int64  `json:"id"`
	Name string `json:"name"` // file name as shown to the user
	Path string `json:"path"` // absolute path on disk to the stored unique copy

	Ext      string   `json:"ext"`  // wav, mp3, flac...
	Size     int64    `json:"size"` // bytes
	Category Category `json:"category"`
	Auto     bool     `json:"auto"` // true if category came from audio analysis (not name)
	Origin   Origin   `json:"origin"`

	// Source breadcrumb — the archive / project / folder it came from.
	SourceLabel string `json:"sourceLabel"`
	SourcePath  string `json:"sourcePath"`

	// Hashes & fingerprints for the multi-level dedup engine.
	MD5         string `json:"md5"`
	SHA256      string `json:"sha256"`
	Fingerprint string `json:"fingerprint"` // perceptual audio hash (hex)

	Features AudioFeatures `json:"features"`

	// Library metadata.
	BPM        int       `json:"bpm,omitempty"`
	KeyName    string    `json:"key,omitempty"`
	Tags       []string  `json:"tags"`
	Favorite   bool      `json:"favorite"`
	Rating     int       `json:"rating"` // 0..5
	UsedCount  int       `json:"usedCount"`
	AddedAt    time.Time `json:"addedAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// Project is an FL Studio project parsed from a .flp file.
type Project struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Title     string    `json:"title"` // project title from FLP metadata
	Artist    string    `json:"artist"`
	BPM       float64   `json:"bpm"`
	KeyName   string    `json:"key,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	Tags      []string  `json:"tags"`

	// Parsed contents.
	SamplePaths []string     `json:"samplePaths"`
	Plugins     []string     `json:"plugins"`
	Channels    []FLPChannel `json:"channels"`
	PPQ         int          `json:"ppq,omitempty"` // тиков на четверть из FLhd
	// Notes содержит ноты пианоролла; не сериализуется в JSON-ответах API —
	// используется только внутри MidiExtractService.
	Notes []FLPNote `json:"-"`

	FLPVersion       string    `json:"flpVersion"`
	Size             int64     `json:"size"`
	AddedAt          time.Time `json:"addedAt"`
	TimeSpentSeconds int64     `json:"timeSpentSeconds"` // cumulative "Time Spent" from Project Info
}

// FLPChannel is a single channel/instrument inside a project.
type FLPChannel struct {
	Index      int    `json:"index"`
	Name       string `json:"name"`
	SamplePath string `json:"samplePath,omitempty"`
	Plugin     string `json:"plugin,omitempty"`
	Kind       string `json:"kind"` // sampler, plugin, automation, layer...
	// IsEmptySampler = true, если канал является нативным сэмплером FL Studio без загруженного звука.
	// Каналы с плагином (Kontakt, Serum) или непустым SamplePath — всегда false.
	IsEmptySampler bool `json:"isEmptySampler,omitempty"`
}

// FLPNote — нота пианоролла FL Studio, разобранная из события FLP_PatternNotes.
// Поля не сериализуются в JSON: тип используется только внутри Go-конвейера.
type FLPNote struct {
	Position     uint32 `json:"-"` // позиция в тиках (из FLhd PPQ)
	Length       uint32 `json:"-"` // длительность в тиках
	RackChan     uint16 `json:"-"` // индекс канала в Channel Rack
	Key          uint8  `json:"-"` // MIDI-номер ноты 0-127
	Velocity     uint8  `json:"-"` // скорость 0-127
	PatternIndex int    `json:"-"` // индекс паттерна
	PatternName  string `json:"-"` // имя паттерна
}

// Collection is a user-defined grouping (a kit, a pack, a mood board).
type Collection struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Note      string    `json:"note"`
	SampleIDs []int64   `json:"sampleIds"`
	CreatedAt time.Time `json:"createdAt"`
}

// DedupStats summarises one harvest run.
type DedupStats struct {
	FilesFound     int   `json:"filesFound"`
	UniqueFiles    int   `json:"uniqueFiles"`
	Duplicates     int   `json:"duplicates"`
	BytesSaved     int64 `json:"bytesSaved"`
	BytesTotal     int64 `json:"bytesTotal"`
	ProjectsParsed int   `json:"projectsParsed"`
	ArchivesOpened int   `json:"archivesOpened"`
}
