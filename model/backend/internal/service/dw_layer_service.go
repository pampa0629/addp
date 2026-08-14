package service

import (
	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/gorm"
)

type DWLayerService struct {
	repo *repository.DWLayerRepository
}

func NewDWLayerService(repo *repository.DWLayerRepository) *DWLayerService {
	return &DWLayerService{repo: repo}
}

func (s *DWLayerService) CreateDWLayer(req *models.CreateDWLayerRequest, tenantID int64) (*models.DWLayer, error) {
	if err := validateCreateDWLayerRequest(req); err != nil {
		return nil, err
	}
	exists, err := s.repo.ExistsByCode(req.LayerCode, tenantID, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, apperrors.Conflict("dw_layer_code_conflict", i18n.MsgLayerCodeConflict)
	}

	layer := &models.DWLayer{
		TenantID:    tenantID,
		LayerCode:   req.LayerCode,
		LayerName:   req.LayerName,
		Description: req.Description,
		NamingRule:  req.NamingRule,
		QualitySLA:  req.QualitySLA,
		SortOrder:   req.SortOrder,
		Version:     1,
	}

	if err := s.repo.Create(layer); err != nil {
		return nil, modelResourceError(err, "dw_layer_code", i18n.MsgLayerCodeConflict)
	}
	return layer, nil
}

func (s *DWLayerService) GetDWLayer(id, tenantID int64) (*models.DWLayer, error) {
	layer, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, modelResourceError(err, "dw_layer_not_found", i18n.MsgLayerNotFound)
	}
	return layer, nil
}

func (s *DWLayerService) ListDWLayers(tenantID int64) ([]models.DWLayer, error) {
	return s.repo.List(tenantID)
}

func (s *DWLayerService) UpdateDWLayer(id, tenantID int64, req *models.UpdateDWLayerRequest) (*models.DWLayer, error) {
	if req == nil || !validRequiredString(req.LayerName, 100) || req.SortOrder == nil || *req.SortOrder < 0 {
		return nil, apperrors.Validation("invalid_request", i18n.MsgValidationFailed)
	}
	var layer *models.DWLayer
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		var err error
		layer, err = repository.LockDWLayer(tx, id, tenantID)
		if err != nil {
			return modelResourceError(err, "dw_layer_not_found", i18n.MsgLayerNotFound)
		}
		if err := requireVersion(layer.Version, req.Version); err != nil {
			return err
		}
		layer.LayerName = req.LayerName
		layer.Description = req.Description
		layer.NamingRule = req.NamingRule
		layer.QualitySLA = req.QualitySLA
		layer.SortOrder = *req.SortOrder
		return repository.NewDWLayerRepository(tx).Update(layer)
	})
	if err != nil {
		return nil, err
	}
	return layer, nil
}

func (s *DWLayerService) DeleteDWLayer(id, tenantID, version int64) error {
	return s.repo.DB().Transaction(func(tx *gorm.DB) error {
		layer, err := repository.LockDWLayer(tx, id, tenantID)
		if err != nil {
			return modelResourceError(err, "dw_layer_not_found", i18n.MsgLayerNotFound)
		}
		if err := requireVersion(layer.Version, version); err != nil {
			return err
		}
		txRepo := repository.NewDWLayerRepository(tx)
		count, err := txRepo.CountLogicalTableReferences(layer.LayerCode, tenantID)
		if err != nil {
			return err
		}
		if count > 0 {
			return apperrors.Conflict("dw_layer_in_use", i18n.MsgLayerInUse)
		}
		return txRepo.Delete(id, tenantID, version)
	})
}
