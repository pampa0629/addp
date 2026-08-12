package service

import (
	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
)

type DWLayerService struct {
	repo *repository.DWLayerRepository
}

func NewDWLayerService(repo *repository.DWLayerRepository) *DWLayerService {
	return &DWLayerService{repo: repo}
}

func (s *DWLayerService) CreateDWLayer(req *models.CreateDWLayerRequest, tenantID int64) (*models.DWLayer, error) {
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
	layer, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, modelResourceError(err, "dw_layer_not_found", i18n.MsgLayerNotFound)
	}

	if req.LayerName != "" {
		layer.LayerName = req.LayerName
	}
	layer.Description = req.Description
	layer.NamingRule = req.NamingRule
	if req.QualitySLA != nil {
		layer.QualitySLA = req.QualitySLA
	}
	if req.SortOrder != nil {
		layer.SortOrder = *req.SortOrder
	}

	if err := s.repo.Update(layer); err != nil {
		return nil, err
	}
	return layer, nil
}

func (s *DWLayerService) DeleteDWLayer(id, tenantID int64) error {
	layer, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return modelResourceError(err, "dw_layer_not_found", i18n.MsgLayerNotFound)
	}
	count, err := s.repo.CountLogicalTableReferences(layer.LayerCode, tenantID)
	if err != nil {
		return err
	}
	if count > 0 {
		return apperrors.Conflict("dw_layer_in_use", i18n.MsgLayerInUse)
	}
	return s.repo.Delete(id, tenantID)
}
