package http

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/flapp/core/internal/domain"
)

// handleMidiExtract запускает фоновое извлечение MIDI-клипов из FLP-проектов.
func (s *Server) handleMidiExtract(w http.ResponseWriter, r *http.Request) {
	var req domain.MidiExtractRequest
	if err := decodeJSON(r, &req); err != nil || len(req.Inputs) == 0 {
		log.Printf("[midi] handleMidiExtract: bad request err=%v inputs=%v", err, req.Inputs)
		writeError(w, domain.ErrInvalidInput)
		return
	}
	log.Printf("[midi] handleMidiExtract: queuing job, inputs=%d", len(req.Inputs))
	jobID := s.svc.Jobs.Enqueue(domain.JobMidiExtract, func(ctx context.Context, report domain.ProgressReporter) (map[string]interface{}, error) {
		result, err := s.svc.MidiExtract.Extract(ctx, req, report)
		if err != nil {
			log.Printf("[midi] Extract job error: %v", err)
		}
		return result, err
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": jobID})
}

// handleMidiClips возвращает список извлечённых клипов с опциональным фильтром.
func (s *Server) handleMidiClips(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")
	clips := s.svc.MidiExtract.ListClips(category)
	if clips == nil {
		clips = []*domain.MidiClip{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": clips,
		"total": len(clips),
	})
}

// handleMidiClipFile отдаёт .mid файл клипа для скачивания/превью.
func (s *Server) handleMidiClipFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	path, err := s.svc.MidiExtract.GetClipFile(id)
	if err != nil {
		log.Printf("[midi] GetClipFile id=%q: %v", id, err)
		writeError(w, err)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		log.Printf("[midi] open .mid %q: %v", path, err)
		writeError(w, domain.ErrNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, err)
		return
	}
	base := filepath.Base(path)
	w.Header().Set("Content-Disposition", `attachment; filename="`+base+`"`)
	w.Header().Set("Content-Type", "audio/midi")
	http.ServeContent(w, r, base, info.ModTime(), f)
}

// handleMidiClipSetCategory задаёт категорию клипа вручную.
// Пак (BuildPack) использует текущее значение Category — поэтому override
// автоматически учитывается при сборке ZIP без дополнительных изменений.
func (s *Server) handleMidiClipSetCategory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Category string `json:"category"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Category == "" {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	if err := s.svc.MidiExtract.SetClipCategory(id, domain.MidiCategory(body.Category)); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleMidiPack собирает выбранные .mid файлы в ZIP и запускает как фоновую задачу.
// OutputDir из тела запроса имеет приоритет → settings.MidiOutputDir → settings.ExportDir → дефолт.
func (s *Server) handleMidiPack(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs       []string `json:"ids"`
		PackName  string   `json:"packName"`
		OutputDir string   `json:"outputDir"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.IDs) == 0 {
		log.Printf("[midi] handleMidiPack: bad request err=%v ids=%v", err, body.IDs)
		writeError(w, domain.ErrInvalidInput)
		return
	}
	cfg := s.svc.Settings.Get()
	exportDir := body.OutputDir
	if exportDir == "" {
		exportDir = cfg.MidiOutputDir
	}
	if exportDir == "" {
		exportDir = cfg.ExportDir
	}
	log.Printf("[midi] handleMidiPack: ids=%d packName=%q exportDir=%q", len(body.IDs), body.PackName, exportDir)
	jobID := s.svc.Jobs.Enqueue(domain.JobExportPack, func(ctx context.Context, report domain.ProgressReporter) (map[string]interface{}, error) {
		path, err := s.svc.MidiExtract.BuildPack(ctx, body.IDs, body.PackName, exportDir)
		if err != nil {
			log.Printf("[midi] BuildPack error: %v", err)
			return nil, err
		}
		return map[string]interface{}{"path": path}, nil
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": jobID})
}

// handleMidiClipNotes парсит .mid файл клипа и отдаёт ноты для пианоролла.
func (s *Server) handleMidiClipNotes(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	result, err := s.svc.MidiExtract.GetClipNotes(id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleMidiClipSample отдаёт аудиофайл сэмпла привязанного к клипу.
// Если файл не найден по оригинальному пути из FLP — ищет в библиотеке звуков по имени файла.
func (s *Server) handleMidiClipSample(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	samplePath, err := s.svc.MidiExtract.GetClipSamplePath(id)
	if err != nil {
		// Оригинальный путь из FLP не существует — ищем в библиотеке по имени файла.
		rawPath := s.svc.MidiExtract.GetClipRawSamplePath(id)
		if rawPath == "" {
			writeError(w, domain.ErrNotFound)
			return
		}
		base := filepath.Base(rawPath)
		nameNoExt := strings.TrimSuffix(base, filepath.Ext(base))
		libResult, libErr := s.svc.Library.Search(r.Context(), domain.SearchQuery{
			Text:  nameNoExt,
			Limit: 10,
		})
		if libErr != nil || len(libResult.Items) == 0 {
			writeError(w, domain.ErrNotFound)
			return
		}
		// Предпочитаем точное совпадение по имени файла (Name = "kick.wav").
		baseLow := strings.ToLower(base)
		samplePath = libResult.Items[0].Path
		for _, item := range libResult.Items {
			if strings.ToLower(item.Name) == baseLow ||
				strings.ToLower(filepath.Base(item.Path)) == baseLow {
				samplePath = item.Path
				break
			}
		}
	}
	f, err := os.Open(samplePath)
	if err != nil {
		log.Printf("[midi] open sample %q: %v", samplePath, err)
		writeError(w, domain.ErrNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, err)
		return
	}
	ext := strings.ToLower(filepath.Ext(samplePath))
	ct := "audio/wav"
	switch ext {
	case ".mp3":
		ct = "audio/mpeg"
	case ".flac":
		ct = "audio/flac"
	case ".ogg":
		ct = "audio/ogg"
	case ".aif", ".aiff":
		ct = "audio/aiff"
	}
	w.Header().Set("Content-Type", ct)
	http.ServeContent(w, r, filepath.Base(samplePath), info.ModTime(), f)
}

// handleMidiDedup удаляет дубликаты MIDI-клипов (одинаковый контент нот).
func (s *Server) handleMidiDedup(w http.ResponseWriter, r *http.Request) {
	result := s.svc.MidiExtract.DeduplicateClips()
	writeJSON(w, http.StatusOK, result)
}

// handleMidiClear удаляет все клипы из памяти сервиса.
func (s *Server) handleMidiClear(w http.ResponseWriter, r *http.Request) {
	s.svc.MidiExtract.ClearClips()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleCacheClear очищает производный кэш приложения:
//   - .mid файлы из midiDir (извлечённые из FLP)
//   - .zip файлы из exportDir (собранные паки)
//
// НЕ трогает: library.db, settings.json, library/ (уникальные аудиокопии).
func (s *Server) handleCacheClear(w http.ResponseWriter, r *http.Request) {
	stats, err := s.svc.MidiExtract.ClearDerivedCache()
	if err != nil {
		log.Printf("[cache] ClearDerivedCache error: %v", err)
		writeError(w, err)
		return
	}
	log.Printf("[cache] cleared: midi=%d files (%d B), exports=%d files (%d B)",
		stats.MidiFiles, stats.MidiBytes, stats.ExportFiles, stats.ExportBytes)
	writeJSON(w, http.StatusOK, stats)
}
