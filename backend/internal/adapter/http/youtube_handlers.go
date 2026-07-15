package http

import (
	"context"
	"net/http"

	"github.com/flapp/core/internal/domain"
	"github.com/flapp/core/internal/usecase"
)

// --- YouTube publishing ---

// handleYouTubeStatus reports OAuth configuration/connection for the UI.
func (s *Server) handleYouTubeStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.YouTube.Client.Status())
}

// handleYouTubeAuth opens the Google consent screen and waits for the loopback
// redirect in the background. UI опрашивает /status до появления connected.
func (s *Server) handleYouTubeAuth(w http.ResponseWriter, r *http.Request) {
	authURL, err := s.svc.YouTube.Client.StartAuth()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"authUrl": authURL})
}

func (s *Server) handleYouTubeDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.YouTube.Client.Disconnect(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleYouTubeFfmpeg reports whether a usable ffmpeg binary was found.
func (s *Server) handleYouTubeFfmpeg(w http.ResponseWriter, r *http.Request) {
	path, err := s.svc.YouTube.FFmpegPath()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"found": false, "path": ""})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": true, "path": path})
}

// handleYouTubeFfmpegDownload запускает джобу скачивания портативного ffmpeg
// в папку данных приложения; путь прописывается в настройки автоматически.
func (s *Server) handleYouTubeFfmpegDownload(w http.ResponseWriter, r *http.Request) {
	jobID := s.svc.Jobs.Enqueue(domain.JobFfmpegFetch, func(ctx context.Context, report domain.ProgressReporter) (map[string]interface{}, error) {
		return s.svc.YouTube.DownloadFFmpeg(ctx, report)
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": jobID})
}

// handleYouTubeTags returns an auto-generated tag list for the given type
// artists (query param "artists", через запятую).
func (s *Server) handleYouTubeTags(w http.ResponseWriter, r *http.Request) {
	artists := queryCSV(r, "artists")
	if len(artists) == 0 {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	tags := s.svc.YouTube.SuggestTags(r.Context(), artists)
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

// --- Cover images (Pinterest) ---

// handleCoversSearch proxies a Pinterest pin search for the cover picker.
func (s *Server) handleCoversSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	items, err := s.svc.Covers.Search(r.Context(), q, queryInt(r, "limit", 40))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleCoversDownload stores the chosen image locally so ffmpeg can use it.
func (s *Server) handleCoversDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(r, &req); err != nil || req.URL == "" {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	path, err := s.svc.Covers.Download(r.Context(), req.URL)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}

// handleYouTubeUpload enqueues one render+upload job per call and returns its
// id; очередь по битам формирует фронтенд, сервис сериализует выполнение.
func (s *Server) handleYouTubeUpload(w http.ResponseWriter, r *http.Request) {
	var req usecase.YouTubeUploadRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	if req.AudioPath == "" || req.ImagePath == "" || req.Title == "" {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	jobID := s.svc.Jobs.Enqueue(domain.JobYouTube, func(ctx context.Context, report domain.ProgressReporter) (map[string]interface{}, error) {
		return s.svc.YouTube.Upload(ctx, req, report)
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": jobID})
}

// handleYouTubePreview renders a short still-video clip (no upload) and returns
// its local path so the UI can play the final look with the burned-in text.
func (s *Server) handleYouTubePreview(w http.ResponseWriter, r *http.Request) {
	var req usecase.YouTubeUploadRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	if req.AudioPath == "" || req.ImagePath == "" {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	path, err := s.svc.YouTube.Preview(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}
