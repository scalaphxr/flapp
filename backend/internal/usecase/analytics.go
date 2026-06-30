package usecase

import (
	"context"

	"github.com/flapp/core/internal/domain"
)

// AnalyticsService exposes the dashboard aggregates.
type AnalyticsService struct {
	repo domain.AnalyticsRepository
}

// NewAnalyticsService wires an analytics service.
func NewAnalyticsService(repo domain.AnalyticsRepository) *AnalyticsService {
	return &AnalyticsService{repo: repo}
}

// Overview returns the full analytics snapshot.
func (s *AnalyticsService) Overview(ctx context.Context) (*domain.Analytics, error) {
	return s.repo.Overview(ctx)
}
