package service

import (
	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/gorm"
)

type DimensionHierarchyService struct {
	repo      *repository.DimensionHierarchyRepository
	tableRepo *repository.LogicalTableRepository
}

func NewDimensionHierarchyService(repo *repository.DimensionHierarchyRepository, tableRepo *repository.LogicalTableRepository) *DimensionHierarchyService {
	return &DimensionHierarchyService{repo: repo, tableRepo: tableRepo}
}

func (s *DimensionHierarchyService) List(tableID, tenantID int64) ([]models.DimensionHierarchy, error) {
	table, err := s.tableRepo.GetByID(tableID, tenantID)
	if err != nil {
		return nil, apperrors.NotFound("logical_table_not_found", i18n.MsgTableNotFound)
	}
	if table.TableType != "dimension" {
		return nil, apperrors.Validation("dimension_table_required", i18n.MsgDimensionTableRequired)
	}
	return s.repo.List(tableID, tenantID)
}

func (s *DimensionHierarchyService) Create(tableID, tenantID int64, req *models.CreateDimensionHierarchyRequest) (*models.DimensionHierarchyMutationResponse, error) {
	if req == nil || req.Version <= 0 || !validRequiredString(req.Name, 200) {
		return nil, invalidRequest()
	}
	response := &models.DimensionHierarchyMutationResponse{}
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		table, err := lockDraftDimensionTable(tx, tableID, tenantID, req.Version)
		if err != nil {
			return err
		}
		response.Hierarchy = models.DimensionHierarchy{
			TenantID: tenantID, TableID: tableID, Name: req.Name, Description: req.Description,
			Levels: []models.DimensionHierarchyLevel{},
		}
		if err := repository.NewDimensionHierarchyRepository(tx).Create(&response.Hierarchy); err != nil {
			return modelResourceError(err, "dimension_hierarchy_name", i18n.MsgDimensionHierarchyConflict)
		}
		response.Version, err = repository.AdvanceLogicalTableVersion(tx, tableID, tenantID, table.Version)
		return err
	})
	return response, err
}

func (s *DimensionHierarchyService) Update(id, tableID, tenantID int64, req *models.UpdateDimensionHierarchyRequest) (*models.DimensionHierarchyMutationResponse, error) {
	if req == nil || req.Version <= 0 || !validRequiredString(req.Name, 200) {
		return nil, invalidRequest()
	}
	response := &models.DimensionHierarchyMutationResponse{}
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		table, err := lockDraftDimensionTable(tx, tableID, tenantID, req.Version)
		if err != nil {
			return err
		}
		txRepo := repository.NewDimensionHierarchyRepository(tx)
		item, err := txRepo.GetByID(id, tableID, tenantID)
		if err != nil {
			return apperrors.NotFound("dimension_hierarchy_not_found", i18n.MsgDimensionHierarchyNotFound)
		}
		item.Name = req.Name
		item.Description = req.Description
		if err := txRepo.Update(item); err != nil {
			return modelResourceError(err, "dimension_hierarchy_name", i18n.MsgDimensionHierarchyConflict)
		}
		response.Hierarchy = *item
		response.Version, err = repository.AdvanceLogicalTableVersion(tx, tableID, tenantID, table.Version)
		return err
	})
	return response, err
}

func (s *DimensionHierarchyService) Delete(id, tableID, tenantID, version int64) (*models.VersionResponse, error) {
	response := &models.VersionResponse{}
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		table, err := lockDraftDimensionTable(tx, tableID, tenantID, version)
		if err != nil {
			return err
		}
		if err := repository.NewDimensionHierarchyRepository(tx).Delete(id, tableID, tenantID); err != nil {
			return apperrors.NotFound("dimension_hierarchy_not_found", i18n.MsgDimensionHierarchyNotFound)
		}
		response.Version, err = repository.AdvanceLogicalTableVersion(tx, tableID, tenantID, table.Version)
		return err
	})
	return response, err
}

func (s *DimensionHierarchyService) CreateLevel(hierarchyID, tableID, tenantID int64, req *models.UpsertDimensionHierarchyLevelRequest) (*models.DimensionHierarchyLevelMutationResponse, error) {
	if !validDimensionHierarchyLevelRequest(req) {
		return nil, invalidRequest()
	}
	response := &models.DimensionHierarchyLevelMutationResponse{}
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		table, err := lockDraftDimensionTable(tx, tableID, tenantID, req.Version)
		if err != nil {
			return err
		}
		txRepo := repository.NewDimensionHierarchyRepository(tx)
		if _, err := txRepo.GetByID(hierarchyID, tableID, tenantID); err != nil {
			return apperrors.NotFound("dimension_hierarchy_not_found", i18n.MsgDimensionHierarchyNotFound)
		}
		if _, err := repository.NewLogicalTableRepository(tx).GetFieldByID(req.FieldID, tableID); err != nil {
			return apperrors.NotFound("dimension_hierarchy_field_not_found", i18n.MsgDimensionHierarchyFieldNotFound)
		}
		response.Level = models.DimensionHierarchyLevel{
			HierarchyID: hierarchyID, FieldID: req.FieldID, LevelNum: req.LevelNum, LevelName: req.LevelName,
		}
		if err := txRepo.CreateLevel(&response.Level); err != nil {
			return modelResourceError(err, "dimension_hierarchy_level", i18n.MsgDimensionHierarchyLevelConflict)
		}
		response.Version, err = repository.AdvanceLogicalTableVersion(tx, tableID, tenantID, table.Version)
		return err
	})
	return response, err
}

func (s *DimensionHierarchyService) UpdateLevel(levelID, hierarchyID, tableID, tenantID int64, req *models.UpsertDimensionHierarchyLevelRequest) (*models.DimensionHierarchyLevelMutationResponse, error) {
	if !validDimensionHierarchyLevelRequest(req) {
		return nil, invalidRequest()
	}
	response := &models.DimensionHierarchyLevelMutationResponse{}
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		table, err := lockDraftDimensionTable(tx, tableID, tenantID, req.Version)
		if err != nil {
			return err
		}
		txRepo := repository.NewDimensionHierarchyRepository(tx)
		if _, err := txRepo.GetByID(hierarchyID, tableID, tenantID); err != nil {
			return apperrors.NotFound("dimension_hierarchy_not_found", i18n.MsgDimensionHierarchyNotFound)
		}
		if _, err := repository.NewLogicalTableRepository(tx).GetFieldByID(req.FieldID, tableID); err != nil {
			return apperrors.NotFound("dimension_hierarchy_field_not_found", i18n.MsgDimensionHierarchyFieldNotFound)
		}
		level, err := txRepo.GetLevelByID(levelID, hierarchyID)
		if err != nil {
			return apperrors.NotFound("dimension_hierarchy_level_not_found", i18n.MsgDimensionHierarchyLevelNotFound)
		}
		level.FieldID = req.FieldID
		level.LevelNum = req.LevelNum
		level.LevelName = req.LevelName
		if err := txRepo.UpdateLevel(level); err != nil {
			return modelResourceError(err, "dimension_hierarchy_level", i18n.MsgDimensionHierarchyLevelConflict)
		}
		response.Level = *level
		response.Version, err = repository.AdvanceLogicalTableVersion(tx, tableID, tenantID, table.Version)
		return err
	})
	return response, err
}

func (s *DimensionHierarchyService) DeleteLevel(levelID, hierarchyID, tableID, tenantID, version int64) (*models.VersionResponse, error) {
	response := &models.VersionResponse{}
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		table, err := lockDraftDimensionTable(tx, tableID, tenantID, version)
		if err != nil {
			return err
		}
		txRepo := repository.NewDimensionHierarchyRepository(tx)
		if _, err := txRepo.GetByID(hierarchyID, tableID, tenantID); err != nil {
			return apperrors.NotFound("dimension_hierarchy_not_found", i18n.MsgDimensionHierarchyNotFound)
		}
		if err := txRepo.DeleteLevel(levelID, hierarchyID); err != nil {
			return apperrors.NotFound("dimension_hierarchy_level_not_found", i18n.MsgDimensionHierarchyLevelNotFound)
		}
		response.Version, err = repository.AdvanceLogicalTableVersion(tx, tableID, tenantID, table.Version)
		return err
	})
	return response, err
}

func lockDraftDimensionTable(tx *gorm.DB, tableID, tenantID, version int64) (*models.LogicalTable, error) {
	table, err := repository.LockLogicalTable(tx, tableID, tenantID)
	if err != nil {
		return nil, apperrors.NotFound("logical_table_not_found", i18n.MsgTableNotFound)
	}
	if err := requireVersion(table.Version, version); err != nil {
		return nil, err
	}
	if table.Status != "draft" {
		return nil, apperrors.Conflict("logical_table_state_conflict", i18n.MsgTableStateConflict)
	}
	if table.TableType != "dimension" {
		return nil, apperrors.Validation("dimension_table_required", i18n.MsgDimensionTableRequired)
	}
	return table, nil
}

func validDimensionHierarchyLevelRequest(req *models.UpsertDimensionHierarchyLevelRequest) bool {
	return req != nil && req.Version > 0 && req.FieldID > 0 && req.LevelNum > 0 && validRequiredString(req.LevelName, 100)
}
