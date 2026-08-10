package service

import (
	"errors"
	"fmt"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
)

var ErrInvalidHierarchyLevelNumber = errors.New("invalid dimension hierarchy level number")

// DimensionHierarchyService 维度层级服务
type DimensionHierarchyService struct {
	repo *repository.DimensionHierarchyRepository
	refs *repository.TenantReferenceRepository
}

func NewDimensionHierarchyService(repo *repository.DimensionHierarchyRepository, refs *repository.TenantReferenceRepository) *DimensionHierarchyService {
	return &DimensionHierarchyService{repo: repo, refs: refs}
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
		return nil, fmt.Errorf("层级编码 '%s' 已存在", req.Code)
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
	if err := s.repo.Update(h); err != nil {
		return nil, err
	}
	return h, nil
}

func (s *DimensionHierarchyService) Delete(id, tenantID int64) error {
	return s.repo.Delete(id, tenantID)
}

// --- 层级管理 ---

func (s *DimensionHierarchyService) GetLevels(hierarchyID, tenantID int64) ([]models.DimensionHierarchyLevel, error) {
	return s.repo.GetLevels(hierarchyID, tenantID)
}

func (s *DimensionHierarchyService) CreateLevel(hierarchyID, tenantID int64, req *models.UpsertHierarchyLevelRequest) (*models.DimensionHierarchyLevel, error) {
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
		return nil, fmt.Errorf("层级编号 %d 已存在", req.LevelNum)
	}
	level := &models.DimensionHierarchyLevel{
		HierarchyID: hierarchyID,
		LevelNum:    req.LevelNum,
		Name:        req.Name,
		ElementID:   req.ElementID,
		Description: req.Description,
		SortOrder:   req.SortOrder,
	}
	if err := s.repo.CreateLevel(level); err != nil {
		return nil, err
	}
	return level, nil
}

func (s *DimensionHierarchyService) UpdateLevel(levelID, hierarchyID, tenantID int64, req *models.UpsertHierarchyLevelRequest) (*models.DimensionHierarchyLevel, error) {
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
		return nil, fmt.Errorf("层级编号 %d 已存在", req.LevelNum)
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
	if err := s.repo.UpdateLevel(level); err != nil {
		return nil, err
	}
	return level, nil
}

func (s *DimensionHierarchyService) DeleteLevel(levelID, hierarchyID, tenantID int64) error {
	return s.repo.DeleteLevel(levelID, hierarchyID, tenantID)
}
