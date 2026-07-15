// Package settings persists user-facing application settings as a small JSON
// file in the data directory. It is concurrency-safe and always returns a fully
// populated struct (missing fields fall back to defaults), so the rest of the
// app never has to reason about a partially configured state.
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Settings is the full set of user preferences exposed in the UI.
type Settings struct {
	Language       string `json:"language"`       // "ru" | "en"
	Theme          string `json:"theme"`          // "warm-dark" (default)
	ExportDir      string `json:"exportDir"`      // where sample packs are written
	MidiOutputDir  string `json:"midiOutputDir"`  // where .mid files and MIDI packs go
	Workers        int    `json:"workers"`        // analysis concurrency
	DedupThreshold int    `json:"dedupThreshold"` // acoustic Hamming threshold
	DeepDedup      bool   `json:"deepDedup"`      // enable acoustic dedup
	GenerateTags   bool   `json:"generateTags"`   // auto tag on harvest
	GPU            bool   `json:"gpu"`            // reserved for future ML acceleration
	AutoUpdate     bool   `json:"autoUpdate"`     // check for updates on launch
	BackupOnExit   bool   `json:"backupOnExit"`   // copy the DB on close

	// YouTube publishing (TunesToTube-style): OAuth-креды пользователя из
	// Google Cloud и дефолты формы загрузки, чтобы публикация была в пару кликов.
	FfmpegPath      string `json:"ffmpegPath"`      // путь к ffmpeg ("" = авто-поиск)
	YtClientID      string `json:"ytClientId"`      // OAuth client id (Desktop app)
	YtClientSecret  string `json:"ytClientSecret"`  // OAuth client secret
	YtNickname      string `json:"ytNickname"`      // ник/тег продюсера: подставляется как {nick} и вычищается из {name}
	YtNoTextOverlay bool   `json:"ytNoTextOverlay"` // инвертирован: false = вшивать текст (название+ник) в кадр (вкл по умолчанию)
	YtFont          string `json:"ytFont"`          // шрифт наложения: ключ (arial, impact…) или путь к .ttf; "" = дефолт
	// Память правок распознавания авторов: токен(lowercase) → каноничное имя;
	// пустая строка = «это не автор». Парсинг на фронте, бэкенд только хранит.
	YtAuthorAliases map[string]string `json:"ytAuthorAliases"`
	YtDefaultImage  string `json:"ytDefaultImage"`  // обложка по умолчанию
	YtTitleTemplate string `json:"ytTitleTemplate"` // активный шаблон названия: {name} {type} {bpm} {key} {nick}
	// Сохранённые пресеты шаблонов названия — переключаются в диалоге загрузки.
	YtTitleTemplates []string `json:"ytTitleTemplates"`
	YtDescription    string   `json:"ytDescription"` // активное описание: те же подстановки, что в названии
	// Сохранённые пресеты описаний — переключаются в диалоге загрузки.
	YtDescTemplates []string `json:"ytDescTemplates"`
	YtTags          string   `json:"ytTags"`    // теги по умолчанию, через запятую
	YtPrivacy       string   `json:"ytPrivacy"` // public | unlisted | private
}

// Defaults returns the baseline configuration.
func Defaults() Settings {
	return Settings{
		Language:       "ru",
		Theme:          "warm-dark",
		ExportDir:      "",
		Workers:        0, // 0 = авто-определение по числу ядер (GOMAXPROCS-1)
		DedupThreshold: 80,
		DeepDedup:      true,
		GenerateTags:   true,
		GPU:            false,
		AutoUpdate:     true,
		BackupOnExit:   false,

		YtTitleTemplate: `[FREE] {type} Type Beat "{name}" | {bpm} BPM {key}`,
		YtTitleTemplates: []string{
			`[FREE] {type} Type Beat "{name}" | {bpm} BPM {key}`,
			`{name} | {type} type beat {bpm}bpm {key}`,
			`[FREE] {type} x {nick} Type Beat "{name}"`,
			`{type} type beat — {name}`,
		},
		YtDescription: defaultDescription,
		YtDescTemplates: []string{
			defaultDescription,
		},
		YtTags:    "type beat, instrumental, beat, free type beat",
		YtPrivacy: "public",
	}
}

// defaultDescription — готовый шаблон описания для тайп-бита. Подстановки те же,
// что и в названии, плюс {nick} — тег продюсера из настроек.
const defaultDescription = `{type} Type Beat "{name}"

Prod. {nick}
{bpm} BPM | Key: {key}

Free for non-profit use only — you MUST credit (prod. {nick}) in your title.
For profit use / exclusive rights: contact me.

{type} type beat, {name} type beat, {bpm} bpm, {key}
#typebeat #{nick}`

// Store reads and writes the settings file under a mutex.
type Store struct {
	mu   sync.RWMutex
	path string
	cur  Settings
}

// Open loads settings from path, creating the file with defaults if absent.
func Open(path string) (*Store, error) {
	s := &Store{path: path, cur: Defaults()}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err := s.persist(); err != nil {
				return nil, err
			}
			return s, nil
		}
		return nil, err
	}
	var loaded Settings
	if err := json.Unmarshal(data, &loaded); err == nil {
		s.cur = mergeDefaults(loaded)
	}
	return s, nil
}

// Get returns a copy of the current settings.
func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

// Set replaces the settings and persists them, returning the stored copy.
func (s *Store) Set(next Settings) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur = mergeDefaults(next)
	if err := s.persist(); err != nil {
		return s.cur, err
	}
	return s.cur, nil
}

// persist writes the current settings; callers hold the lock.
func (s *Store) persist() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	// Прошлую версию храним рядом (.bak): единственный шанс восстановить
	// поля вроде OAuth-ключей, если их затрёт кривой PUT.
	if prev, err := os.ReadFile(s.path); err == nil {
		_ = os.WriteFile(s.path+".bak", prev, 0o644)
	}
	data, err := json.MarshalIndent(s.cur, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// mergeDefaults fills zero-valued required fields with their defaults.
func mergeDefaults(in Settings) Settings {
	d := Defaults()
	if in.Language == "" {
		in.Language = d.Language
	}
	if in.Theme == "" {
		in.Theme = d.Theme
	}
	if in.Workers <= 0 {
		in.Workers = d.Workers
	}
	if in.DedupThreshold <= 0 {
		in.DedupThreshold = d.DedupThreshold
	}
	if in.YtTitleTemplate == "" {
		in.YtTitleTemplate = d.YtTitleTemplate
	}
	// Пустой список пресетов не оставляем — дропдауну в диалоге нужен выбор.
	if len(in.YtTitleTemplates) == 0 {
		in.YtTitleTemplates = d.YtTitleTemplates
	}
	if len(in.YtDescTemplates) == 0 {
		in.YtDescTemplates = d.YtDescTemplates
	}
	if in.YtAuthorAliases == nil {
		in.YtAuthorAliases = map[string]string{}
	}
	if in.YtPrivacy == "" {
		in.YtPrivacy = d.YtPrivacy
	}
	return in
}
