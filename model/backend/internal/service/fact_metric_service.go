package service

import (
	"context"

	commonClient "github.com/addp/common/client"

	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/gorm"
)

type FactMetricService struct {
	factMetricRepo *repository.FactMetricRepository
	tableRepo      *repository.LogicalTableRepository
	standard       *commonClient.StandardClient
}

func (s *FactMetricService) SetStandardClient(client *commonClient.StandardClient) {
	s.standard = client
}

func NewFactMetricService(factMetricRepo *repository.FactMetricRepository, tableRepo *repository.LogicalTableRepository) *FactMetricService {
	return &FactMetricService{
		factMetricRepo: factMetricRepo,
		tableRepo:      tableRepo,
	}
}

// ListMetrics 获取事实表关联的指标列表
func (s *FactMetricService) ListMetrics(factTableID, tenantID int64) ([]models.FactMetricMapping, error) {
	// 验证事实表存在且属于该租户
	table, err := s.tableRepo.GetByID(factTableID, tenantID)
	if err != nil {
		return nil, apperrors.NotFound("logical_table_not_found", i18n.MsgTableNotFound)
	}
	if table.TableType != "fact" {
		return nil, apperrors.Validation("fact_table_required", i18n.MsgValidationFailed)
	}
	return s.factMetricRepo.ListByFactTable(factTableID, tenantID)
}

// AddMetric 为事实表添加指标关联
func (s *FactMetricService) AddMetric(factTableID, tenantID, userID int64, req *models.CreateFactMetricMappingRequest) (*models.FactMetricMutationResponse, error) {
	if err := validateCreateFactMetricRequest(req); err != nil {
		return nil, err
	}
	if s.standard != nil {
		if err := s.standard.WithTenantID(uint(tenantID)).ValidateMetric(context.Background(), req.MetricID); err != nil {
			return nil, standardReferenceError(err, "metric_not_found")
		}
	}
	m := models.FactMetricMapping{
		TenantID:    tenantID,
		FactTableID: factTableID,
		MetricID:    req.MetricID,
		FieldID:     req.FieldID,
		Note:        req.Note,
		CreatedBy:   userID,
	}
	response := &models.FactMetricMutationResponse{Mapping: m}
	err := s.factMetricRepo.DB().Transaction(func(tx *gorm.DB) error {
		if err := lockStandardReferences(tx, tenantID, requiredStandardReference(models.StandardResourceMetric, req.MetricID)); err != nil {
			return err
		}
		table, err := repository.LockLogicalTable(tx, factTableID, tenantID)
		if err != nil {
			return apperrors.NotFound("logical_table_not_found", i18n.MsgTableNotFound)
		}
		if err := requireVersion(table.Version, req.Version); err != nil {
			return err
		}
		if table.TableType != "fact" {
			return apperrors.Validation("fact_table_required", i18n.MsgValidationFailed)
		}
		if table.Status != "draft" {
			return apperrors.Conflict("fact_metric_state_conflict", i18n.MsgTableStateConflict)
		}
		if req.FieldID != nil {
			if _, err := repository.NewLogicalTableRepository(tx).GetFieldByID(*req.FieldID, factTableID); err != nil {
				return apperrors.NotFound("logical_field_not_found", i18n.MsgFieldNotFound)
			}
		}
		txRepo := repository.NewFactMetricRepository(tx)
		exists, err := txRepo.Exists(factTableID, req.MetricID, tenantID)
		if err != nil {
			return err
		}
		if exists {
			return apperrors.Conflict("fact_metric_conflict", i18n.MsgMetricConflict)
		}
		if err := txRepo.Create(&response.Mapping); err != nil {
			return modelResourceError(err, "fact_metric", i18n.MsgMetricConflict)
		}
		response.Version, err = repository.AdvanceLogicalTableVersion(tx, factTableID, tenantID, req.Version)
		return err
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

// RemoveMetric 移除事实表的指标关联
func (s *FactMetricService) RemoveMetric(mappingID, factTableID, tenantID, version int64) (*models.VersionResponse, error) {
	response := &models.VersionResponse{}
	err := s.factMetricRepo.DB().Transaction(func(tx *gorm.DB) error {
		table, err := repository.LockLogicalTable(tx, factTableID, tenantID)
		if err != nil {
			return apperrors.NotFound("logical_table_not_found", i18n.MsgTableNotFound)
		}
		if err := requireVersion(table.Version, version); err != nil {
			return err
		}
		if table.TableType != "fact" || table.Status != "draft" {
			return apperrors.Conflict("fact_metric_state_conflict", i18n.MsgTableStateConflict)
		}
		if err := repository.NewFactMetricRepository(tx).Delete(mappingID, factTableID, tenantID); err != nil {
			return modelResourceError(err, "fact_metric_mapping_not_found", i18n.MsgInvalidMappingID)
		}
		response.Version, err = repository.AdvanceLogicalTableVersion(tx, factTableID, tenantID, version)
		return err
	})
	return response, err
}
