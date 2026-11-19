package service

import (
	"context"
	"strings"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

const searchHistoryLimit = 50

// SearchHistoryService manages the current tenant user's search history.
type SearchHistoryService struct {
	repo *repository.SearchHistoryRepository
}

func NewSearchHistoryService(repo *repository.SearchHistoryRepository) *SearchHistoryService {
	return &SearchHistoryService{repo: repo}
}

func (s *SearchHistoryService) Record(ctx context.Context, tenantID, userID uint, query string) error {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return nil
	}
	return s.repo.Record(ctx, tenantID, userID, trimmed, searchHistoryLimit)
}

func (s *SearchHistoryService) List(ctx context.Context, tenantID, userID uint, limit int) ([]models.SearchHistory, error) {
	return s.repo.List(ctx, tenantID, userID, min(limit, searchHistoryLimit))
}

func (s *SearchHistoryService) Delete(ctx context.Context, tenantID, userID, historyID uint) error {
	return s.repo.DeleteByID(ctx, tenantID, userID, historyID)
}

func (s *SearchHistoryService) Clear(ctx context.Context, tenantID, userID uint) error {
	return s.repo.DeleteAll(ctx, tenantID, userID)
}
