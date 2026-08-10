package service

import (
	"context"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
)

type IssueService struct {
	issueRepo *repository.IssueRepository
}

func NewIssueService(issueRepo *repository.IssueRepository) *IssueService {
	return &IssueService{issueRepo: issueRepo}
}

func (s *IssueService) List(tenantID int64, status string, engineID int64, page, pageSize int) ([]models.Issue, int64, error) {
	return s.issueRepo.List(tenantID, status, engineID, page, pageSize)
}

func (s *IssueService) ListByExecution(executionID string) ([]models.Issue, error) {
	return s.issueRepo.ListByExecution(executionID)
}

func (s *IssueService) Get(id, tenantID int64) (*models.Issue, error) {
	return s.issueRepo.Get(id, tenantID)
}

func (s *IssueService) UpdateStatus(ctx context.Context, id, tenantID, userID int64, status, note string) error {
	return s.issueRepo.UpdateStatus(ctx, id, tenantID, userID, status, note)
}
