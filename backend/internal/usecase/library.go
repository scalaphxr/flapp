package usecase

import (
	"context"

	"github.com/flapp/core/internal/domain"
)

// LibraryService is the read/curate surface over the stored sample catalogue.
// It is a thin application layer over the repositories: it exists so the HTTP
// adapter depends on a use-case boundary rather than reaching into storage
// directly, keeping the dependency arrows pointing inward.
type LibraryService struct {
	samples domain.SampleRepository
	tags    domain.TagRepository
}

// NewLibraryService wires a library service.
func NewLibraryService(samples domain.SampleRepository, tags domain.TagRepository) *LibraryService {
	return &LibraryService{samples: samples, tags: tags}
}

// SearchResult is a page of samples plus the total match count.
type SearchResult struct {
	Items []*domain.Sample `json:"items"`
	Total int              `json:"total"`
}

// Search runs a filtered, paginated query.
func (s *LibraryService) Search(ctx context.Context, q domain.SearchQuery) (SearchResult, error) {
	items, total, err := s.samples.Search(ctx, q)
	if err != nil {
		return SearchResult{}, err
	}
	return SearchResult{Items: items, Total: total}, nil
}

// Get returns one sample by id.
func (s *LibraryService) Get(ctx context.Context, id int64) (*domain.Sample, error) {
	return s.samples.GetByID(ctx, id)
}

// Similar returns acoustically nearby samples.
func (s *LibraryService) Similar(ctx context.Context, id int64, limit int) ([]*domain.Sample, error) {
	return s.samples.Similar(ctx, id, limit)
}

// SetCategory overwrites the sample category (manual override clears the auto flag).
func (s *LibraryService) SetCategory(ctx context.Context, id int64, cat string) error {
	return s.samples.SetCategory(ctx, id, cat)
}

// SetFavorite toggles the favorite flag.
func (s *LibraryService) SetFavorite(ctx context.Context, id int64, fav bool) error {
	return s.samples.SetFavorite(ctx, id, fav)
}

// SetRating sets a 0..5 star rating.
func (s *LibraryService) SetRating(ctx context.Context, id int64, rating int) error {
	return s.samples.SetRating(ctx, id, rating)
}

// SetTags replaces a sample's tags.
func (s *LibraryService) SetTags(ctx context.Context, id int64, tags []string) error {
	return s.samples.SetTags(ctx, id, tags)
}

// Delete removes a sample from the library.
func (s *LibraryService) Delete(ctx context.Context, id int64) error {
	return s.samples.Delete(ctx, id)
}

// ClearAll removes every sample from the library.
func (s *LibraryService) ClearAll(ctx context.Context) error {
	return s.samples.DeleteAll(ctx)
}

// AllTags returns the global tag vocabulary with counts.
func (s *LibraryService) AllTags(ctx context.Context) ([]domain.TagCount, error) {
	return s.tags.AllTags(ctx)
}

// GetPeaksJSON возвращает закешированный JSON пиков из БД.
func (s *LibraryService) GetPeaksJSON(ctx context.Context, id int64) (string, error) {
	return s.samples.GetPeaksJSON(ctx, id)
}

// SetPeaksJSON сохраняет JSON пиков в БД.
func (s *LibraryService) SetPeaksJSON(ctx context.Context, id int64, j string) error {
	return s.samples.SetPeaksJSON(ctx, id, j)
}

// GetPeaks2JSON возвращает закешированный JSON пар [min,max] пиков (v2) из БД.
func (s *LibraryService) GetPeaks2JSON(ctx context.Context, id int64) (string, error) {
	return s.samples.GetPeaks2JSON(ctx, id)
}

// SetPeaks2JSON сохраняет JSON пар [min,max] пиков (v2) в БД.
func (s *LibraryService) SetPeaks2JSON(ctx context.Context, id int64, j string) error {
	return s.samples.SetPeaks2JSON(ctx, id, j)
}
