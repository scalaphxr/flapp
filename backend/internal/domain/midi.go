package domain

// MidiCategory — категория MIDI-клипа, определяемая по источнику-каналу.
type MidiCategory string

const (
	MidiCat808Bass MidiCategory = "808/Bass"
	MidiCatMelody  MidiCategory = "Melody"
	// Конкретные ударные (из infrastructure/classify) — по убыванию специфичности.
	MidiCatKick    MidiCategory = "Kick"
	MidiCatSnare   MidiCategory = "Snare"
	MidiCatClap    MidiCategory = "Clap"
	MidiCatHiHat   MidiCategory = "Hi-Hat"
	MidiCatOpenHat MidiCategory = "Open Hat"
	MidiCatPerc    MidiCategory = "Perc"
	// Общий фолбэк для явных ударных без конкретного типа.
	MidiCatDrums MidiCategory = "Drums"
	MidiCatFX    MidiCategory = "FX"
	MidiCatOther MidiCategory = "Other"
)

// AllMidiCategories — упорядоченный список для фильтров и UI-чипов.
var AllMidiCategories = []MidiCategory{
	MidiCat808Bass,
	MidiCatMelody,
	MidiCatKick, MidiCatSnare, MidiCatClap,
	MidiCatHiHat, MidiCatOpenHat, MidiCatPerc, MidiCatDrums,
	MidiCatFX,
	MidiCatOther,
}

// MidiClip — один MIDI-клип: ноты одного канала из одного паттерна FLP.
type MidiClip struct {
	ID           string       `json:"id"`
	ProjectPath  string       `json:"projectPath"`
	ProjectName  string       `json:"projectName"`
	BPM          float64      `json:"bpm"`
	PatternIndex int          `json:"patternIndex"`
	PatternName  string       `json:"patternName"`
	ChannelIndex int          `json:"channelIndex"`
	// ChannelName — реальное имя канала из FL Studio. Пустая строка = нет имени.
	// Фолбэк "Channel N" НЕ хранится здесь — формируется на фронте для отображения.
	ChannelName string       `json:"channelName"`
	SamplePath  string       `json:"samplePath,omitempty"`
	Plugin      string       `json:"plugin,omitempty"`
	Category         MidiCategory `json:"category"`
	CategoryOverride bool         `json:"categoryOverride"` // true = задана пользователем вручную
	DecisionSrc string       `json:"decisionSource"` // "name" | "sample" | "notes"
	NoteCount   int          `json:"noteCount"`
	DurationTicks uint32     `json:"durationTicks"`
	DurationSec   float64    `json:"durationSec"`
	MinKey        uint8      `json:"minKey"`
	MaxKey        uint8      `json:"maxKey"`
	FilePath      string     `json:"filePath,omitempty"`
	FileName      string     `json:"fileName"`
	// SourceType — тип входящего файла из которого извлечён клип: "flp" или "zip".
	SourceType string `json:"sourceType"`
	// SourceName — отображаемое имя источника (без расширения и числового префикса).
	// Для zip показывается имя архива, для flp — имя файла.
	SourceName string `json:"sourceName"`
	// ContentHash — короткий хеш содержимого нот (нормализован по стартовой позиции).
	// Используется для детекта дубликатов. Пустая строка = нет нот.
	ContentHash string `json:"contentHash,omitempty"`
}

// MidiDedupResult — результат операции удаления дубликатов.
type MidiDedupResult struct {
	Removed int              `json:"removed"`
	Groups  int              `json:"groups"`  // количество групп дубликатов
	Kept    []*MidiClip      `json:"kept"`    // представители каждой группы (для отчёта)
}

// MidiExtractRequest — запрос на извлечение MIDI из FLP-проектов.
type MidiExtractRequest struct {
	// Inputs — список путей к .flp-файлам, ZIP/RAR/7Z-архивам или папкам.
	Inputs []string `json:"inputs"`
	// OutputDir — целевая папка для .mid файлов (пустая = используется внутренняя).
	OutputDir string `json:"outputDir,omitempty"`
	// IgnoreEmptySamplers — пропускать каналы-сэмплеры без загруженного звука.
	// true = фильтровать (рекомендуется); false = включать все группы нот.
	IgnoreEmptySamplers bool `json:"ignoreEmptySamplers"`
}

// MidiNote — одна нота для превью пианоролла.
type MidiNote struct {
	Tick          int `json:"tick"`
	DurationTicks int `json:"durationTicks"`
	Pitch         int `json:"pitch"`
	Velocity      int `json:"velocity"`
}

// MidiNotesResult — ответ GET /api/midi/clips/{id}/notes.
type MidiNotesResult struct {
	BPM           float64    `json:"bpm"`
	TicksPerBeat  int        `json:"ticksPerBeat"`
	DurationTicks int        `json:"durationTicks"`
	Notes         []MidiNote `json:"notes"`
}

// CacheStats — статистика очищенного кэша.
type CacheStats struct {
	MidiFiles    int   `json:"midiFiles"`
	MidiBytes    int64 `json:"midiBytes"`
	ExportFiles  int   `json:"exportFiles"`
	ExportBytes  int64 `json:"exportBytes"`
	TotalBytes   int64 `json:"totalBytes"`
}
