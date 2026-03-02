package service

import (
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
)

type IssueService struct {
	issueRepo *repository.IssueRepository
}

func NewIssueService(issueRepo *repository.IssueRepository) *IssueService {
	return &IssueService{issueRepo: issueRepo}
}

func (s *IssueService) List(tenantID int64, status string, engineID int64) ([]models.Issue, error) {
	return s.issueRepo.List(tenantID, status, engineID)
}

func (s *IssueService) ListByExecution(executionID string) ([]models.Issue, error) {
	return s.issueRepo.ListByExecution(executionID)
}

func (s *IssueService) Get(id, tenantID int64) (*models.Issue, error) {
	return s.issueRepo.Get(id, tenantID)
}

func (s *IssueService) UpdateStatus(id, tenantID int64, status string) error {
	return s.issueRepo.UpdateStatus(id, tenantID, status)
}
