package service

import (
	"strings"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

const (
	defaultHistoryKeep  = 50
	maxHistoryFetchSize = 50
)

// SearchHistoryService 封装搜索历史的业务逻辑
type SearchHistoryService struct {
	repo      *repository.SearchHistoryRepository
	itemsKeep int
}

func NewSearchHistoryService(repo *repository.SearchHistoryRepository) *SearchHistoryService {
	return &SearchHistoryService{
		repo:      repo,
		itemsKeep: defaultHistoryKeep,
	}
}

// Record 记录用户的搜索关键词，忽略空白输入
func (s *SearchHistoryService) Record(userID uint, tenantID *uint, query string) error {
	if userID == 0 {
		return nil
	}
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return nil
	}

	if err := s.repo.Upsert(userID, tenantID, trimmed); err != nil {
		return err
	}

	if s.itemsKeep > 0 {
		if err := s.repo.TrimByUser(userID, s.itemsKeep); err != nil {
			return err
		}
	}
	return nil
}

// List 返回指定用户近期的历史记录
func (s *SearchHistoryService) List(userID uint, limit int) ([]models.SearchHistory, error) {
	if limit <= 0 || limit > maxHistoryFetchSize {
		limit = maxHistoryFetchSize
	}
	return s.repo.List(userID, limit)
}

// Delete 删除用户的单条历史
func (s *SearchHistoryService) Delete(userID, historyID uint) error {
	if userID == 0 || historyID == 0 {
		return nil
	}
	return s.repo.DeleteByID(userID, historyID)
}

// Clear 移除用户的所有历史记录
func (s *SearchHistoryService) Clear(userID uint) error {
	if userID == 0 {
		return nil
	}
	return s.repo.DeleteAll(userID)
}
