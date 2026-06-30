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
	}
}

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
	return in
}
