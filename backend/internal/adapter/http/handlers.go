package http

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/flapp/core/internal/domain"
	"github.com/flapp/core/internal/infrastructure/audio"
	"github.com/flapp/core/internal/infrastructure/settings"
	"github.com/flapp/core/internal/usecase"
)

// --- Harvest & jobs ---

func (s *Server) handleHarvest(w http.ResponseWriter, r *http.Request) {
	var req domain.HarvestRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	if len(req.Inputs) == 0 {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	// Apply the configured acoustic threshold unless the request overrides it.
	if req.AcousticThreshold == 0 {
		req.AcousticThreshold = s.svc.Settings.Get().DedupThreshold
	}

	jobID := s.svc.Jobs.Enqueue(domain.JobHarvest, func(ctx context.Context, report domain.ProgressReporter) (map[string]interface{}, error) {
		return s.svc.Harvest.Run(ctx, req, report)
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": jobID})
}

func (s *Server) handleJobsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.Jobs.List())
}

func (s *Server) handleJobGet(w http.ResponseWriter, r *http.Request) {
	job, ok := s.svc.Jobs.Get(r.PathValue("id"))
	if !ok {
		writeError(w, domain.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleJobCancel(w http.ResponseWriter, r *http.Request) {
	ok := s.svc.Jobs.Cancel(r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]bool{"canceled": ok})
}

// --- Library ---

func (s *Server) handleSamplesSearch(w http.ResponseWriter, r *http.Request) {
	q := domain.SearchQuery{
		Text:       r.URL.Query().Get("q"),
		Categories: toCategories(queryCSV(r, "categories")),
		Origins:    toOrigins(queryCSV(r, "origins")),
		Tags:       queryCSV(r, "tags"),
		MinBPM:     queryInt(r, "minBpm", 0),
		MaxBPM:     queryInt(r, "maxBpm", 0),
		MinSize:    int64(queryInt(r, "minSize", 0)),
		MaxSize:    int64(queryInt(r, "maxSize", 0)),
		FavOnly:    r.URL.Query().Get("favorite") == "true",
		MinRating:  queryInt(r, "minRating", 0),
		Sort:       r.URL.Query().Get("sort"),
		Order:      r.URL.Query().Get("order"),
		Limit:      queryInt(r, "limit", 100),
		Offset:     queryInt(r, "offset", 0),
	}
	// Recognise RU/EN mood & instrument words in free text (e.g. "тёмные 808",
	// "тёплый бас") the same way Smart Search does, so the plain search box
	// benefits too — without this, a Cyrillic query never matches anything
	// (sample names/tags are stored in English), since it only ran behind the
	// separate opt-in "Smart Search" wand button. Only applies when the
	// caller hasn't already set structured filters explicitly.
	if q.Text != "" && len(q.Categories) == 0 && len(q.Tags) == 0 && q.MinBPM == 0 && q.MaxBPM == 0 {
		parsed, _ := s.svc.Smart.Parse(q.Text)
		q.Text = parsed.Text
		q.Categories = parsed.Categories
		q.Tags = parsed.Tags
		q.MinBPM = parsed.MinBPM
		q.MaxBPM = parsed.MaxBPM
	}
	res, err := s.svc.Library.Search(r.Context(), q)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSampleGet(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	smp, err := s.svc.Library.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, smp)
}

func (s *Server) handleSamplePeaks(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	// 1500 бинов — достаточное разрешение для чёткой волны на всю ширину строки
	// без пикселизации даже при Retina-дисплеях (физических пикселей ~2×).
	const bins = 1500

	smp, err := s.svc.Library.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	// Сначала проверяем кэш v2 (пары [min,max]) в БД — дёшево, без декодирования.
	if cached, cerr := s.svc.Library.GetPeaks2JSON(r.Context(), id); cerr == nil && cached != "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"peaks":` + cached + `}`))
		return
	}

	// Кэш пуст — декодируем пары min/max из файла.
	peaks, err := audio.PeakMinMax(smp.Path, bins)
	if err != nil || len(peaks) == 0 {
		// Неподдерживаемый формат (m4a/aac) или ошибка чтения: пустой массив.
		writeJSON(w, http.StatusOK, map[string]interface{}{"peaks": [][2]float64{}})
		return
	}

	// Сохраняем в БД для последующих запросов (ошибку сохранения игнорируем).
	if peaksJSON, merr := json.Marshal(peaks); merr == nil {
		_ = s.svc.Library.SetPeaks2JSON(r.Context(), id, string(peaksJSON))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"peaks": peaks})
}

func (s *Server) handleSampleSpectrogram(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	frames := queryInt(r, "frames", 200)
	if frames < 20 {
		frames = 20
	}
	if frames > 600 {
		frames = 600
	}
	bins := queryInt(r, "bins", 64)
	if bins < 8 {
		bins = 8
	}
	if bins > 200 {
		bins = 200
	}
	smp, err := s.svc.Library.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	res, err := audio.ComputeSpectrogram(smp.Path, frames, bins)
	if err != nil || res == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": []float64{}, "frames": 0, "bins": 0})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSampleSimilar(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	items, err := s.svc.Library.Similar(r.Context(), id, queryInt(r, "limit", 20))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleSampleAudio streams the raw audio file for in-app preview and waveform
// rendering. http.ServeFile honours Range requests so the player can seek.
func (s *Server) handleSampleAudio(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	smp, err := s.svc.Library.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	f, err := os.Open(smp.Path)
	if err != nil {
		writeError(w, domain.ErrNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, err)
		return
	}
	http.ServeContent(w, r, smp.Name, info.ModTime(), f)
}

func (s *Server) handleSampleCategory(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	var body struct {
		Category string `json:"category"`
	}
	_ = decodeJSON(r, &body)
	if body.Category == "" {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	if err := s.svc.Library.SetCategory(r.Context(), id, body.Category); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSampleFavorite(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	var body struct {
		Favorite bool `json:"favorite"`
	}
	_ = decodeJSON(r, &body)
	if err := s.svc.Library.SetFavorite(r.Context(), id, body.Favorite); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSampleRating(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	var body struct {
		Rating int `json:"rating"`
	}
	_ = decodeJSON(r, &body)
	if err := s.svc.Library.SetRating(r.Context(), id, body.Rating); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSampleTags(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	var body struct {
		Tags []string `json:"tags"`
	}
	_ = decodeJSON(r, &body)
	if err := s.svc.Library.SetTags(r.Context(), id, body.Tags); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSampleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	if err := s.svc.Library.Delete(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSamplesClear(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.Library.ClearAll(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Projects ---

func (s *Server) handleProjectsSearch(w http.ResponseWriter, r *http.Request) {
	items, total, err := s.svc.Projects.Search(r.Context(),
		r.URL.Query().Get("q"), queryInt(r, "limit", 100), queryInt(r, "offset", 0))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (s *Server) handleProjectGet(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	p, err := s.svc.Projects.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// --- Collections ---

func (s *Server) handleCollectionsList(w http.ResponseWriter, r *http.Request) {
	cols, err := s.svc.Collections.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": cols})
}

func (s *Server) handleCollectionCreate(w http.ResponseWriter, r *http.Request) {
	var c domain.Collection
	if err := decodeJSON(r, &c); err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	id, err := s.svc.Collections.Create(r.Context(), &c)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *Server) handleCollectionGet(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	c, err := s.svc.Collections.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleCollectionAddSamples(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	var body struct {
		SampleIDs []int64 `json:"sampleIds"`
	}
	_ = decodeJSON(r, &body)
	if err := s.svc.Collections.AddSamples(r.Context(), id, body.SampleIDs); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleCollectionRemoveSample(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	sid, err := pathID(r, "sid")
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	if err := s.svc.Collections.RemoveSample(r.Context(), id, sid); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleCollectionDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	if err := s.svc.Collections.Delete(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Beat manager (rename) ---

func (s *Server) handleRenamePreview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs  []int64            `json:"ids"`
		Spec usecase.RenameSpec `json:"spec"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	previews, err := s.svc.BeatMgr.Preview(r.Context(), body.IDs, body.Spec)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": previews})
}

func (s *Server) handleRenameApply(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs  []int64            `json:"ids"`
		Spec usecase.RenameSpec `json:"spec"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	jobID := s.svc.Jobs.Enqueue(domain.JobRename, func(ctx context.Context, report domain.ProgressReporter) (map[string]interface{}, error) {
		return s.svc.BeatMgr.Apply(ctx, body.IDs, body.Spec, report)
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": jobID})
}

// handleListFiles returns all non-directory entries in a given folder
// (non-recursive). Used by the Beat Manager file-source panel.
func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	type fileItem struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	items := make([]fileItem, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			name := e.Name()
			items = append(items, fileItem{Path: filepath.Join(dir, name), Name: name})
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

func (s *Server) handleRenameFilesPreview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paths []string           `json:"paths"`
		Spec  usecase.RenameSpec `json:"spec"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.Paths) == 0 {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	items, err := s.svc.BeatMgr.PreviewFiles(r.Context(), body.Paths, body.Spec)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

func (s *Server) handleRenameFilesApply(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paths []string           `json:"paths"`
		Spec  usecase.RenameSpec `json:"spec"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.Paths) == 0 {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	jobID := s.svc.Jobs.Enqueue(domain.JobRename, func(ctx context.Context, report domain.ProgressReporter) (map[string]interface{}, error) {
		return s.svc.BeatMgr.ApplyFiles(ctx, body.Paths, body.Spec, report)
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": jobID})
}

// --- Pack builder ---

func (s *Server) handlePackBuild(w http.ResponseWriter, r *http.Request) {
	var req usecase.PackRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	if len(req.SampleIDs) == 0 {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	jobID := s.svc.Jobs.Enqueue(domain.JobExportPack, func(ctx context.Context, report domain.ProgressReporter) (map[string]interface{}, error) {
		return s.svc.PackBuild.Build(ctx, req, report)
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": jobID})
}

func (s *Server) handleExportFolder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SampleIDs []int64 `json:"sampleIds"`
		DestDir   string  `json:"destDir"`
	}
	if err := decodeJSON(r, &body); err != nil || body.DestDir == "" || len(body.SampleIDs) == 0 {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	jobID := s.svc.Jobs.Enqueue(domain.JobExportPack, func(ctx context.Context, report domain.ProgressReporter) (map[string]interface{}, error) {
		return s.svc.PackBuild.ExportToFolder(ctx, body.SampleIDs, body.DestDir, report)
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": jobID})
}

// --- Analytics, tags, smart search ---

func (s *Server) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	a, err := s.svc.Analytics.Overview(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.svc.Library.AllTags(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": tags})
}

func (s *Server) handleSmartSearch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query  string `json:"query"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	res, err := s.svc.Smart.Search(r.Context(), body.Query, body.Limit, body.Offset)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// --- Settings ---

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.Settings.Get())
}

func (s *Server) handleSettingsPut(w http.ResponseWriter, r *http.Request) {
	var next settings.Settings
	if err := decodeJSON(r, &next); err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	saved, err := s.svc.Settings.Set(next)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

// --- conversion helpers ---

func toCategories(ss []string) []domain.Category {
	if len(ss) == 0 {
		return nil
	}
	out := make([]domain.Category, 0, len(ss))
	for _, s := range ss {
		out = append(out, domain.Category(s))
	}
	return out
}

func toOrigins(ss []string) []domain.Origin {
	if len(ss) == 0 {
		return nil
	}
	out := make([]domain.Origin, 0, len(ss))
	for _, s := range ss {
		out = append(out, domain.Origin(s))
	}
	return out
}
