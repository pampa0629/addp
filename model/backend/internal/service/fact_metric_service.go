package service

import (
	"context"

	commonClient "github.com/addp/common/client"

	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
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
func (s *FactMetricService) AddMetric(factTableID, tenantID, userID int64, req *models.CreateFactMetricMappingRequest) (*models.FactMetricMapping, error) {
	// 验证事实表存在且属于该租户
	table, err := s.tableRepo.GetByID(factTableID, tenantID)
	if err != nil {
		return nil, apperrors.NotFound("logical_table_not_found", i18n.MsgTableNotFound)
	}
	if table.TableType != "fact" {
		return nil, apperrors.Validation("fact_table_required", i18n.MsgValidationFailed)
	}
	if table.Status != "draft" {
		return nil, apperrors.Conflict("fact_metric_state_conflict", i18n.MsgTableStateConflict)
	}
	if s.standard != nil {
		if err := s.standard.WithTenantID(uint(tenantID)).ValidateMetric(context.Background(), req.MetricID); err != nil {
			return nil, standardReferenceError(err, "metric_not_found")
		}
	}
	if req.FieldID != nil {
		if _, err := s.tableRepo.GetFieldByID(*req.FieldID, factTableID); err != nil {
			return nil, apperrors.NotFound("logical_field_not_found", i18n.MsgFieldNotFound)
		}
	}

	// 防止重复关联
	exists, err := s.factMetricRepo.Exists(factTableID, req.MetricID, tenantID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, apperrors.Conflict("fact_metric_conflict", i18n.MsgMetricConflict)
	}

	m := &models.FactMetricMapping{
		TenantID:    tenantID,
		FactTableID: factTableID,
		MetricID:    req.MetricID,
		FieldID:     req.FieldID,
		Note:        req.Note,
		CreatedBy:   userID,
	}
	if err := s.factMetricRepo.Create(m); err != nil {
		return nil, modelResourceError(err, "fact_metric", i18n.MsgMetricConflict)
	}
	return m, nil
}

// RemoveMetric 移除事实表的指标关联
func (s *FactMetricService) RemoveMetric(mappingID, factTableID, tenantID int64) error {
	table, err := s.tableRepo.GetByID(factTableID, tenantID)
	if err != nil {
		return apperrors.NotFound("logical_table_not_found", i18n.MsgTableNotFound)
	}
	if table.TableType != "fact" {
		return apperrors.Validation("fact_table_required", i18n.MsgValidationFailed)
	}
	if table.Status != "draft" {
		return apperrors.Conflict("fact_metric_state_conflict", i18n.MsgTableStateConflict)
	}
	if err := s.factMetricRepo.Delete(mappingID, factTableID, tenantID); err != nil {
		return modelResourceError(err, "fact_metric_mapping_not_found", i18n.MsgInvalidMappingID)
	}
	return nil
}
