package usecase

import (
	"context"
	"net/http"
	"time"

	"github.com/flapp/core/internal/infrastructure/covers"
)

// CoverService finds and stores cover images for YouTube uploads. Поиск идёт по
// публичной картиночной выдаче (пины Pinterest в приоритете, ключи и OAuth не
// нужны); выбранная картинка скачивается в dataDir/covers и дальше используется
// как обычный локальный файл обложки — ffmpeg-рендеру нужен путь на диске.
type CoverService struct {
	HTTP *http.Client
	Dir  string
}

// NewCoverService wires the service with its own short-timeout HTTP client.
func NewCoverService(dir string) *CoverService {
	return &CoverService{
		HTTP: &http.Client{Timeout: 20 * time.Second},
		Dir:  dir,
	}
}

// Search returns cover images for a free-form query (обычно «{артист} aesthetic»).
func (c *CoverService) Search(ctx context.Context, query string, limit int) ([]covers.Image, error) {
	return covers.Search(ctx, c.HTTP, query, limit)
}

// Download stores the image locally and returns its path.
func (c *CoverService) Download(ctx context.Context, url string) (string, error) {
	return covers.Download(ctx, c.HTTP, url, c.Dir)
}
