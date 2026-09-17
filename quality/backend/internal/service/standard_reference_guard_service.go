package service

import (
	"errors"
	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	commonRepository "github.com/addp/common/repository"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
)

type StandardReferenceGuardService struct {
	repo *repository.StandardReferenceGuardRepository
}

func NewStandardReferenceGuardService(repo *repository.StandardReferenceGuardRepository) *StandardReferenceGuardService {
	return &StandardReferenceGuardService{repo: repo}
}
func (s *StandardReferenceGuardService) SetState(tenantID, resourceID int64, state string) (*commonClient.StandardReferenceGuardResponse, error) {
	if tenantID <= 0 || resourceID <= 0 || (state != models.StandardReferenceGuardOpen && state != models.StandardReferenceGuardFrozen && state != models.StandardReferenceGuardDeleted) {
		return nil, commonAPI.ErrBadRequest
	}
	result, err := s.repo.SetState(tenantID, resourceID, state)
	if errors.Is(err, commonRepository.ErrReferenceGuardTerminal) {
		return nil, commonAPI.ErrConflict
	}
	return result, err
}
