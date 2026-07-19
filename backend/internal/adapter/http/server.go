// Package http exposes the application services over a local JSON/HTTP API
// consumed by the Tauri front-end. It uses only the standard library's
// net/http: Go 1.22 pattern routing ("GET /api/samples/{id}") removes any need
// for a third-party router. The server binds to 127.0.0.1, so it is reachable
// only by the desktop shell on the same machine.
package http

import (
	"encoding/json"
	"net/http"
	_ "net/http/pprof" // регистрирует /debug/pprof/* на http.DefaultServeMux
	"strconv"
	"strings"

	"github.com/flapp/core/internal/domain"
	"github.com/flapp/core/internal/infrastructure/settings"
	"github.com/flapp/core/internal/usecase"
)


// Services bundles every dependency the HTTP handlers need. Construction lives
// in main; the handlers depend on use-case types and domain ports only.
type Services struct {
	Library     *usecase.LibraryService
	Harvest     *usecase.HarvestService
	BeatMgr     *usecase.BeatManagerService
	PackBuild   *usecase.PackBuilderService
	Analytics   *usecase.AnalyticsService
	Smart       *usecase.SmartSearchService
	MidiExtract *usecase.MidiExtractService
	YouTube     *usecase.YouTubeService
	Covers      *usecase.CoverService
	Projects    domain.ProjectRepository
	Collections domain.CollectionRepository
	Jobs        domain.JobQueue
	Settings    *settings.Store
}

// Server wires the services to an HTTP mux.
type Server struct {
	svc Services
	mux *http.ServeMux
}

// New builds a Server and registers all routes.
func New(svc Services) *Server {
	s := &Server{svc: svc, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler returns the root handler with CORS applied.
func (s *Server) Handler() http.Handler {
	return withCORS(s.mux)
}

// routes registers every endpoint.
func (s *Server) routes() {
	m := s.mux

	m.HandleFunc("GET /api/health", s.handleHealth)

	// pprof: доступен только на localhost, безопасно для desktop-сайдкара.
	// Использование: curl "http://127.0.0.1:PORT/debug/pprof/profile?seconds=30" -o cpu.pprof
	//               go tool pprof -top cpu.pprof
	m.Handle("/debug/pprof/", http.DefaultServeMux)

	// Harvest + jobs.
	m.HandleFunc("POST /api/harvest", s.handleHarvest)
	m.HandleFunc("GET /api/jobs", s.handleJobsList)
	m.HandleFunc("GET /api/jobs/{id}", s.handleJobGet)
	m.HandleFunc("POST /api/jobs/{id}/cancel", s.handleJobCancel)
	m.HandleFunc("GET /api/events", s.handleEvents)

	// Library.
	m.HandleFunc("GET /api/samples", s.handleSamplesSearch)
	m.HandleFunc("GET /api/samples/{id}", s.handleSampleGet)
	m.HandleFunc("GET /api/samples/{id}/similar", s.handleSampleSimilar)
	m.HandleFunc("GET /api/samples/{id}/peaks", s.handleSamplePeaks)
	m.HandleFunc("GET /api/samples/{id}/spectrogram", s.handleSampleSpectrogram)
	m.HandleFunc("GET /api/samples/{id}/audio", s.handleSampleAudio)
	m.HandleFunc("POST /api/samples/{id}/category", s.handleSampleCategory)
	m.HandleFunc("POST /api/samples/{id}/favorite", s.handleSampleFavorite)
	m.HandleFunc("POST /api/samples/{id}/rating", s.handleSampleRating)
	m.HandleFunc("POST /api/samples/{id}/tags", s.handleSampleTags)
	m.HandleFunc("DELETE /api/samples/{id}", s.handleSampleDelete)
	m.HandleFunc("DELETE /api/samples", s.handleSamplesClear)

	// Projects.
	m.HandleFunc("GET /api/projects", s.handleProjectsSearch)
	m.HandleFunc("GET /api/projects/{id}", s.handleProjectGet)

	// Collections.
	m.HandleFunc("GET /api/collections", s.handleCollectionsList)
	m.HandleFunc("POST /api/collections", s.handleCollectionCreate)
	m.HandleFunc("GET /api/collections/{id}", s.handleCollectionGet)
	m.HandleFunc("POST /api/collections/{id}/samples", s.handleCollectionAddSamples)
	m.HandleFunc("DELETE /api/collections/{id}/samples/{sid}", s.handleCollectionRemoveSample)
	m.HandleFunc("DELETE /api/collections/{id}", s.handleCollectionDelete)

	// Beat manager.
	m.HandleFunc("GET /api/files", s.handleListFiles)
	m.HandleFunc("POST /api/rename/preview", s.handleRenamePreview)
	m.HandleFunc("POST /api/rename/apply", s.handleRenameApply)
	m.HandleFunc("POST /api/rename/files/preview", s.handleRenameFilesPreview)
	m.HandleFunc("POST /api/rename/files/apply", s.handleRenameFilesApply)

	// Pack builder.
	m.HandleFunc("POST /api/packs", s.handlePackBuild)
	m.HandleFunc("POST /api/export/folder", s.handleExportFolder)

	// Analytics, tags, smart search.
	m.HandleFunc("GET /api/analytics", s.handleAnalytics)
	m.HandleFunc("GET /api/tags", s.handleTags)
	m.HandleFunc("POST /api/smartsearch", s.handleSmartSearch)

	// Settings.
	m.HandleFunc("GET /api/settings", s.handleSettingsGet)
	m.HandleFunc("PUT /api/settings", s.handleSettingsPut)

	// YouTube publishing.
	m.HandleFunc("GET /api/youtube/status", s.handleYouTubeStatus)
	m.HandleFunc("POST /api/youtube/auth", s.handleYouTubeAuth)
	m.HandleFunc("POST /api/youtube/disconnect", s.handleYouTubeDisconnect)
	m.HandleFunc("GET /api/youtube/ffmpeg", s.handleYouTubeFfmpeg)
	m.HandleFunc("POST /api/youtube/ffmpeg/download", s.handleYouTubeFfmpegDownload)
	m.HandleFunc("POST /api/youtube/upload", s.handleYouTubeUpload)
	m.HandleFunc("POST /api/youtube/preview", s.handleYouTubePreview)
	m.HandleFunc("POST /api/youtube/preview-frame", s.handleYouTubePreviewFrame)
	m.HandleFunc("GET /api/youtube/tags", s.handleYouTubeTags)

	// Cover images (Pinterest search + local download for the renderer).
	m.HandleFunc("GET /api/covers/search", s.handleCoversSearch)
	m.HandleFunc("POST /api/covers/download", s.handleCoversDownload)

	// MIDI extraction.
	m.HandleFunc("POST /api/midi/extract", s.handleMidiExtract)
	m.HandleFunc("GET /api/midi/clips", s.handleMidiClips)
	m.HandleFunc("GET /api/midi/clips/{id}/file", s.handleMidiClipFile)
	m.HandleFunc("GET /api/midi/clips/{id}/notes", s.handleMidiClipNotes)
	m.HandleFunc("GET /api/midi/clips/{id}/sample", s.handleMidiClipSample)
	m.HandleFunc("POST /api/midi/clips/{id}/category", s.handleMidiClipSetCategory)
	m.HandleFunc("POST /api/midi/pack", s.handleMidiPack)
	m.HandleFunc("DELETE /api/midi/clips", s.handleMidiClear)
	m.HandleFunc("POST /api/midi/dedup", s.handleMidiDedup)
	// Производный кэш (midi + exports): не трогает library.db и пользовательские данные.
	m.HandleFunc("POST /api/cache/clear", s.handleCacheClear)
}

// --- shared helpers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// withCORS allows the local desktop webview (any localhost origin) to call the
// API, including preflight requests.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// writeJSON encodes v as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError sends a JSON error body, mapping domain errors to HTTP codes.
func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch err {
	case domain.ErrNotFound:
		status = http.StatusNotFound
	case domain.ErrInvalidInput:
		status = http.StatusBadRequest
	case domain.ErrUnsupported:
		status = http.StatusUnsupportedMediaType
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// decodeJSON reads a JSON request body into v.
func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	return dec.Decode(v)
}

// pathID parses the {id} path value as an int64.
func pathID(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

// queryInt reads an integer query parameter with a fallback.
func queryInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// queryCSV splits a comma-separated query parameter, dropping empties.
func queryCSV(r *http.Request, key string) []string {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
