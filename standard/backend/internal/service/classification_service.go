package service

import (
	"fmt"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
)

// ClassificationService 数据分类服务
type ClassificationService struct {
	repo        *repository.ClassificationRepository
	gradingRepo *repository.GradingLevelRepository
	refs        *repository.TenantReferenceRepository
}

func NewClassificationService(repo *repository.ClassificationRepository, gradingRepo *repository.GradingLevelRepository, refs *repository.TenantReferenceRepository) *ClassificationService {
	return &ClassificationService{repo: repo, gradingRepo: gradingRepo, refs: refs}
}

func (s *ClassificationService) ListClassifications(tenantID int64) ([]models.Classification, error) {
	return s.repo.List(tenantID)
}

func (s *ClassificationService) GetClassification(id, tenantID int64) (*models.Classification, error) {
	return s.repo.GetByID(id, tenantID)
}

func (s *ClassificationService) CreateClassification(req *models.CreateClassificationRequest, tenantID, userID int64) (*models.Classification, error) {
	if err := s.refs.RequireClassification(tenantID, req.ParentID); err != nil {
		return nil, err
	}
	c := &models.Classification{
		TenantID:    tenantID,
		Name:        req.Name,
		Code:        req.Code,
		Description: req.Description,
		ParentID:    req.ParentID,
		SortOrder:   req.SortOrder,
		CreatedBy:   userID,
	}
	if err := s.repo.Create(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *ClassificationService) UpdateClassification(id, tenantID, userID int64, req *models.UpdateClassificationRequest) (*models.Classification, error) {
	c, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	if err := s.validateParent(id, tenantID, req.ParentID); err != nil {
		return nil, err
	}
	if req.Name != "" {
		c.Name = req.Name
	}
	c.Description = req.Description
	c.ParentID = req.ParentID
	c.SortOrder = req.SortOrder
	c.UpdatedBy = &userID
	if err := s.repo.Update(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *ClassificationService) validateParent(id, tenantID int64, parentID *int64) error {
	if err := s.refs.RequireClassification(tenantID, parentID); err != nil {
		return err
	}
	for current := parentID; current != nil; {
		if *current == id {
			return fmt.Errorf("数据分类父级不能是自身或其子级")
		}
		parent, err := s.repo.GetByID(*current, tenantID)
		if err != nil {
			return err
		}
		current = parent.ParentID
	}
	return nil
}

func (s *ClassificationService) DeleteClassification(id, tenantID int64) error {
	return s.repo.Delete(id, tenantID)
}

// --- 数据分级 ---

func (s *ClassificationService) ListGradingLevels(tenantID int64) ([]models.GradingLevel, error) {
	// 确保分级数据存在（懒初始化）
	if err := s.gradingRepo.EnsureDefaults(tenantID); err != nil {
		return nil, err
	}
	return s.gradingRepo.List(tenantID)
}

func (s *ClassificationService) UpdateGradingLevel(id, tenantID int64, req *models.UpdateGradingLevelRequest) error {
	return s.gradingRepo.Update(id, tenantID, req)
}
