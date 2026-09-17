package service

import (
	"context"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
)

type OverviewService struct {
	repo *repository.OverviewRepository
}

func NewOverviewService(repo *repository.OverviewRepository) *OverviewService {
	return &OverviewService{repo: repo}
}
func (s *OverviewService) Get(ctx context.Context, tenantID int64, domainID *int64, days, page, pageSize int) (*models.QualityOverview, error) {
	return s.repo.Get(ctx, tenantID, domainID, days, page, pageSize)
}
