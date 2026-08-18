package service

import (
	"context"
	"errors"

	commonapi "github.com/addp/common/api"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/gorm"
)

var ErrInvalidHierarchyLevelNumber = errors.New("invalid dimension hierarchy level number")

// DimensionHierarchyService 维度层级服务
type DimensionHierarchyService struct {
	repo     *repository.DimensionHierarchyRepository
	refs     *repository.TenantReferenceRepository
	deletion *StandardReferenceDeletionService
}

func NewDimensionHierarchyService(repo *repository.DimensionHierarchyRepository, refs *repository.TenantReferenceRepository, deletion *StandardReferenceDeletionService) *DimensionHierarchyService {
	return &DimensionHierarchyService{repo: repo, refs: refs, deletion: deletion}
}

func (s *DimensionHierarchyService) List(tenantID int64) ([]models.DimensionHierarchy, error) {
	return s.repo.List(tenantID)
}

func (s *DimensionHierarchyService) GetByID(id, tenantID int64) (*models.DimensionHierarchy, error) {
	return s.repo.GetByID(id, tenantID)
}

func (s *DimensionHierarchyService) Create(req *models.CreateDimensionHierarchyRequest, tenantID, userID int64) (*models.DimensionHierarchy, error) {
	if err := s.refs.RequireDomain(tenantID, req.DomainID); err != nil {
		return nil, err
	}
	exists, err := s.repo.ExistsByCode(req.Code, tenantID, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, commonapi.ErrConflict
	}

	h := &models.DimensionHierarchy{
		TenantID:    tenantID,
		DomainID:    req.DomainID,
		Name:        req.Name,
		Code:        req.Code,
		Description: req.Description,
		CreatedBy:   userID,
	}
	if err := s.repo.Create(h); err != nil {
		return nil, err
	}
	return h, nil
}

func (s *DimensionHierarchyService) Update(id, tenantID, userID int64, req *models.UpdateDimensionHierarchyRequest) (*models.DimensionHierarchy, error) {
	h, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	if err := s.refs.RequireDomain(tenantID, req.DomainID); err != nil {
		return nil, err
	}
	if req.Name != "" {
		h.Name = req.Name
	}
	h.DomainID = req.DomainID
	h.Description = req.Description
	h.UpdatedBy = &userID
	if err := s.repo.Update(h, req.Version); err != nil {
		return nil, err
	}
	return s.repo.GetByID(id, tenantID)
}

func (s *DimensionHierarchyService) Delete(ctx context.Context, id, tenantID int64) error {
	return s.deletion.Delete(ctx, tenantID, "dimension_hierarchy", id, func(tx *gorm.DB, resourceID, resourceTenantID int64) error {
		return s.repo.DeleteTx(tx, resourceID, resourceTenantID)
	})
}

// --- 层级管理 ---

func (s *DimensionHierarchyService) GetLevels(hierarchyID, tenantID int64) ([]models.DimensionHierarchyLevel, error) {
	return s.repo.GetLevels(hierarchyID, tenantID)
}

func (s *DimensionHierarchyService) CreateLevel(hierarchyID, tenantID int64, req *models.UpsertHierarchyLevelRequest) (*models.HierarchyLevelMutationResponse, error) {
	if _, err := s.repo.GetByID(hierarchyID, tenantID); err != nil {
		return nil, err
	}
	if req.LevelNum <= 0 {
		return nil, ErrInvalidHierarchyLevelNumber
	}
	if req.ElementID != nil {
		if err := s.refs.RequireElement(tenantID, *req.ElementID); err != nil {
			return nil, err
		}
	}
	exists, err := s.repo.ExistsLevelNum(hierarchyID, tenantID, req.LevelNum, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, commonapi.ErrConflict
	}
	level := &models.DimensionHierarchyLevel{
		HierarchyID: hierarchyID,
		LevelNum:    req.LevelNum,
		Name:        req.Name,
		ElementID:   req.ElementID,
		Description: req.Description,
		SortOrder:   req.SortOrder,
	}
	if err := s.repo.CreateLevel(level, tenantID, req.Version); err != nil {
		return nil, err
	}
	return &models.HierarchyLevelMutationResponse{Level: level, Version: req.Version + 1}, nil
}

func (s *DimensionHierarchyService) UpdateLevel(levelID, hierarchyID, tenantID int64, req *models.UpsertHierarchyLevelRequest) (*models.HierarchyLevelMutationResponse, error) {
	level, err := s.repo.GetLevelByID(levelID, hierarchyID, tenantID)
	if err != nil {
		return nil, err
	}
	if req.LevelNum <= 0 {
		return nil, ErrInvalidHierarchyLevelNumber
	}
	if req.ElementID != nil {
		if err := s.refs.RequireElement(tenantID, *req.ElementID); err != nil {
			return nil, err
		}
	}
	exists, err := s.repo.ExistsLevelNum(hierarchyID, tenantID, req.LevelNum, levelID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, commonapi.ErrConflict
	}
	if req.Name != "" {
		level.Name = req.Name
	}
	if req.LevelNum > 0 {
		level.LevelNum = req.LevelNum
	}
	level.ElementID = req.ElementID
	level.Description = req.Description
	level.SortOrder = req.SortOrder
	if err := s.repo.UpdateLevel(level, tenantID, req.Version); err != nil {
		return nil, err
	}
	return &models.HierarchyLevelMutationResponse{Level: level, Version: req.Version + 1}, nil
}

func (s *DimensionHierarchyService) DeleteLevel(levelID, hierarchyID, tenantID, version int64) error {
	return s.repo.DeleteLevel(levelID, hierarchyID, tenantID, version)
}
