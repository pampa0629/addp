package service

import (
	"fmt"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
)

// UnitService 计量单位服务
type UnitService struct {
	catRepo  *repository.MeasurementCategoryRepository
	unitRepo *repository.UnitRepository
}

func NewUnitService(catRepo *repository.MeasurementCategoryRepository, unitRepo *repository.UnitRepository) *UnitService {
	return &UnitService{catRepo: catRepo, unitRepo: unitRepo}
}

// --- 度量类别 ---

func (s *UnitService) ListCategories(tenantID int64) ([]models.MeasurementCategory, error) {
	return s.catRepo.List(tenantID)
}

func (s *UnitService) CreateCategory(req *models.CreateMeasurementCategoryRequest, tenantID int64) (*models.MeasurementCategory, error) {
	exists, err := s.catRepo.ExistsByCode(req.Code, tenantID, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("度量类别编码 '%s' 已存在", req.Code)
	}
	cat := &models.MeasurementCategory{
		TenantID:    tenantID,
		Name:        req.Name,
		Code:        req.Code,
		Description: req.Description,
		SortOrder:   req.SortOrder,
	}
	if err := s.catRepo.Create(cat); err != nil {
		return nil, err
	}
	return cat, nil
}

func (s *UnitService) UpdateCategory(id, tenantID int64, req *models.UpdateMeasurementCategoryRequest) (*models.MeasurementCategory, error) {
	cat, err := s.catRepo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	if req.Name != "" {
		cat.Name = req.Name
	}
	cat.Description = req.Description
	cat.SortOrder = req.SortOrder
	if err := s.catRepo.Update(cat); err != nil {
		return nil, err
	}
	return cat, nil
}

func (s *UnitService) DeleteCategory(id, tenantID int64) error {
	cat, err := s.catRepo.GetByID(id, tenantID)
	if err != nil {
		return err
	}
	if cat.IsSystem {
		return fmt.Errorf("系统内置度量类别不允许删除")
	}
	return s.catRepo.Delete(id, tenantID)
}

// --- 计量单位 ---

func (s *UnitService) ListUnits(tenantID int64, categoryID *int64) ([]models.Unit, error) {
	return s.unitRepo.List(tenantID, categoryID)
}

func (s *UnitService) GetUnit(id, tenantID int64) (*models.Unit, error) {
	return s.unitRepo.GetByID(id, tenantID)
}

func (s *UnitService) CreateUnit(req *models.CreateUnitRequest, tenantID int64) (*models.Unit, error) {
	// 验证度量类别存在
	if _, err := s.catRepo.GetByID(req.CategoryID, tenantID); err != nil {
		return nil, fmt.Errorf("度量类别不存在")
	}
	unit := &models.Unit{
		TenantID:    tenantID,
		CategoryID:  req.CategoryID,
		Name:        req.Name,
		Symbol:      req.Symbol,
		Description: req.Description,
		SortOrder:   req.SortOrder,
	}
	if err := s.unitRepo.Create(unit); err != nil {
		return nil, err
	}
	return s.unitRepo.GetByID(unit.ID, tenantID)
}

func (s *UnitService) UpdateUnit(id, tenantID int64, req *models.UpdateUnitRequest) (*models.Unit, error) {
	unit, err := s.unitRepo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	if unit.IsSystem {
		return nil, fmt.Errorf("系统内置单位不允许修改")
	}
	if req.Name != "" {
		unit.Name = req.Name
	}
	unit.Symbol = req.Symbol
	unit.Description = req.Description
	unit.SortOrder = req.SortOrder
	if err := s.unitRepo.Update(unit); err != nil {
		return nil, err
	}
	return unit, nil
}

func (s *UnitService) DeleteUnit(id, tenantID int64) error {
	unit, err := s.unitRepo.GetByID(id, tenantID)
	if err != nil {
		return err
	}
	if unit.IsSystem {
		return fmt.Errorf("系统内置单位不允许删除")
	}
	return s.unitRepo.Delete(id, tenantID)
}
