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

func (s *IssueService) List(tenantID int64, status string, engineID int64, ownerDomainID *int64, page, pageSize int) ([]models.Issue, int64, error) {
	return s.issueRepo.List(tenantID, status, engineID, ownerDomainID, page, pageSize)
}

func (s *IssueService) Get(id, tenantID int64) (*models.Issue, error) {
	return s.issueRepo.Get(id, tenantID)
}

func (s *IssueService) UpdateStatus(ctx context.Context, id, tenantID, userID, version int64, status, note string) (*models.Issue, error) {
	return s.issueRepo.UpdateStatus(ctx, id, tenantID, userID, version, status, note)
}
