package service

import (
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
)

type GlossaryService struct {
	repo *repository.GlossaryRepository
}

func NewGlossaryService(repo *repository.GlossaryRepository) *GlossaryService {
	return &GlossaryService{repo: repo}
}

func (s *GlossaryService) CreateGlossary(req *models.CreateGlossaryRequest, tenantID, userID int64) (*models.Glossary, error) {
	glossary := &models.Glossary{
		TenantID:   tenantID,
		DomainID:   req.DomainID,
		Name:       req.Name,
		Alias:      req.Alias,
		Definition: req.Definition,
		Example:    req.Example,
		Note:       req.Note,
		Status:     "draft",
		StewardID:  req.StewardID,
		Tags:       req.Tags,
		CreatedBy:  userID,
	}

	if err := s.repo.Create(glossary); err != nil {
		return nil, err
	}
	return glossary, nil
}

func (s *GlossaryService) GetGlossary(id, tenantID int64) (*models.Glossary, error) {
	return s.repo.GetByID(id, tenantID)
}

func (s *GlossaryService) ListGlossaries(tenantID int64, opts repository.ListGlossaryOptions) ([]models.Glossary, int64, error) {
	return s.repo.List(tenantID, opts)
}

func (s *GlossaryService) UpdateGlossary(id, tenantID, userID int64, req *models.UpdateGlossaryRequest) (*models.Glossary, error) {
	glossary, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		glossary.Name = req.Name
	}
	if req.DomainID != nil {
		glossary.DomainID = req.DomainID
	}
	glossary.Alias = req.Alias
	if req.Definition != "" {
		glossary.Definition = req.Definition
	}
	glossary.Example = req.Example
	glossary.Note = req.Note
	glossary.StewardID = req.StewardID
	glossary.Tags = req.Tags
	glossary.UpdatedBy = &userID

	if err := s.repo.Update(glossary); err != nil {
		return nil, err
	}
	return glossary, nil
}

func (s *GlossaryService) DeleteGlossary(id, tenantID int64) error {
	return s.repo.Delete(id, tenantID)
}

func (s *GlossaryService) ApproveGlossary(id, tenantID, userID int64) error {
	return s.repo.UpdateStatus(id, tenantID, "approved", userID)
}

func (s *GlossaryService) DeprecateGlossary(id, tenantID, userID int64) error {
	return s.repo.UpdateStatus(id, tenantID, "deprecated", userID)
}

// GetMappedElements 获取术语关联的完整数据元列表
func (s *GlossaryService) GetMappedElements(glossaryID, tenantID int64) ([]models.Element, error) {
	return s.repo.GetMappedElements(glossaryID, tenantID)
}

// SetElementMappings 批量替换术语的数据元映射
func (s *GlossaryService) SetElementMappings(glossaryID, tenantID int64, elementIDs []int64) error {
	return s.repo.SetElementMappings(glossaryID, tenantID, elementIDs)
}

// GetGlossariesByElement 根据数据元ID反查关联的术语列表
func (s *GlossaryService) GetGlossariesByElement(elementID, tenantID int64) ([]models.Glossary, error) {
	return s.repo.GetGlossariesByElementID(elementID, tenantID)
}
