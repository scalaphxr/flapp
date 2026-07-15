package usecase

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/flapp/core/internal/domain"
	"github.com/flapp/core/internal/infrastructure/settings"
	"github.com/flapp/core/internal/infrastructure/youtube"
)

// YouTubeUploadRequest describes one beat to publish: аудио + обложка +
// готовые метаданные видео (шаблоны разворачивает фронтенд).
type YouTubeUploadRequest struct {
	AudioPath   string   `json:"audioPath"`
	ImagePath   string   `json:"imagePath"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Privacy     string   `json:"privacy"` // public | unlisted | private
	// Текстовое наложение на кадр видео: название бита (в кавычках) крупно и
	// ник автора помельче ниже. Строки готовит фронтенд, чтобы превью и рендер
	// совпадали.
	Overlay      bool   `json:"overlay"`
	OverlayTitle string `json:"overlayTitle"`
	OverlaySub   string `json:"overlaySub"`
	OverlayFont  string `json:"overlayFont"` // ключ шрифта или путь к .ttf; "" = дефолт
}

// YouTubeService renders a still-image video for a beat and uploads it to the
// connected channel. Работа сериализуется мьютексом: рендер грузит CPU, а
// параллельные аплоады делят один канал — по одному быстрее и предсказуемее.
type YouTubeService struct {
	Client   *youtube.Client
	Settings *settings.Store
	TempDir  string
	// BinDir — куда кладётся автоматически скачанный ffmpeg.exe.
	BinDir string

	mu sync.Mutex
}

// NewYouTubeService wires the service.
func NewYouTubeService(client *youtube.Client, cfg *settings.Store, tempDir, binDir string) *YouTubeService {
	return &YouTubeService{Client: client, Settings: cfg, TempDir: tempDir, BinDir: binDir}
}

// FFmpegPath resolves the ffmpeg binary honouring the settings override.
func (s *YouTubeService) FFmpegPath() (string, error) {
	return youtube.FindFFmpeg(s.Settings.Get().FfmpegPath)
}

// DownloadFFmpeg скачивает портативный ffmpeg в папку данных приложения и
// прописывает его путь в настройки — после этого рендер работает из коробки.
func (s *YouTubeService) DownloadFFmpeg(ctx context.Context, report domain.ProgressReporter) (map[string]interface{}, error) {
	report.Set(0.01, "download", "ffmpeg")
	path, err := youtube.DownloadFFmpeg(ctx, s.BinDir, func(p float64) {
		stage := "download"
		if p >= 0.9 {
			stage = "extract"
		}
		report.Set(p, stage, "ffmpeg")
	})
	if err != nil {
		return nil, err
	}
	cur := s.Settings.Get()
	cur.FfmpegPath = path
	if _, err := s.Settings.Set(cur); err != nil {
		return nil, err
	}
	return map[string]interface{}{"path": path}, nil
}

// SuggestTags auto-builds a tag list around the given "type" artists (шаблонное
// ядро + живые подсказки поиска YouTube). Не требует подключённого канала.
func (s *YouTubeService) SuggestTags(ctx context.Context, artists []string) []string {
	return youtube.GenerateTags(ctx, artists)
}

// Upload runs the full pipeline for one beat: render (≈35% прогресса) → upload
// (остальное). Возвращает id и ссылку на готовое видео.
func (s *YouTubeService) Upload(ctx context.Context, req YouTubeUploadRequest, report domain.ProgressReporter) (map[string]interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req.AudioPath == "" || req.ImagePath == "" {
		return nil, domain.ErrInvalidInput
	}
	for _, p := range []string{req.AudioPath, req.ImagePath} {
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return nil, fmt.Errorf("file not found: %s", p)
		}
	}
	if !s.Client.Status().Connected {
		return nil, errors.New("youtube: channel is not connected (Settings → YouTube)")
	}
	ffmpeg, err := s.FFmpegPath()
	if err != nil {
		return nil, err
	}

	name := filepath.Base(req.AudioPath)
	report.Set(0.01, "render", name)

	out := filepath.Join(s.TempDir, fmt.Sprintf("yt_%d.mp4", time.Now().UnixNano()))
	defer os.Remove(out)
	err = youtube.RenderStill(ctx, ffmpeg, req.ImagePath, req.AudioPath, out, youtube.RenderOpts{
		Overlay:   req.Overlay,
		TitleText: req.OverlayTitle,
		SubText:   req.OverlaySub,
		Font:      req.OverlayFont,
	}, func(p float64) {
		report.Set(0.01+0.34*p, "render", name)
	})
	if err != nil {
		return nil, err
	}

	report.Set(0.35, "upload", req.Title)
	id, err := s.Client.UploadVideo(ctx, out, youtube.UploadMeta{
		Title:       req.Title,
		Description: req.Description,
		Tags:        req.Tags,
		Privacy:     req.Privacy,
	}, func(p float64) {
		report.Set(0.35+0.64*p, "upload", req.Title)
	})
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"videoId": id,
		"url":     "https://youtu.be/" + id,
		"title":   req.Title,
	}, nil
}

// Preview renders a short still-video clip (без загрузки) в фиксированный файл
// во временной папке, чтобы UI показал финальный вид с вшитым текстом. Канал
// подключать не нужно — достаточно ffmpeg, обложки и аудио.
func (s *YouTubeService) Preview(ctx context.Context, req YouTubeUploadRequest) (string, error) {
	if req.AudioPath == "" || req.ImagePath == "" {
		return "", domain.ErrInvalidInput
	}
	for _, p := range []string{req.AudioPath, req.ImagePath} {
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return "", fmt.Errorf("file not found: %s", p)
		}
	}
	ffmpeg, err := s.FFmpegPath()
	if err != nil {
		return "", err
	}
	out := filepath.Join(s.TempDir, "yt_preview.mp4")
	if err := youtube.RenderStill(ctx, ffmpeg, req.ImagePath, req.AudioPath, out, youtube.RenderOpts{
		Overlay:    req.Overlay,
		TitleText:  req.OverlayTitle,
		SubText:    req.OverlaySub,
		Font:       req.OverlayFont,
		MaxSeconds: 15, // короткий клип — превью должно быть быстрым
	}, func(float64) {}); err != nil {
		return "", err
	}
	return out, nil
}
