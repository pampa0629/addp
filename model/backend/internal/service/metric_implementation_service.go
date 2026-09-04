package service

import (
	"context"
	"fmt"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/gorm"
)

type MetricImplementationService struct {
	repo      *repository.MetricImplementationRepository
	tableRepo *repository.LogicalTableRepository
	standard  *commonClient.StandardClient
}

func NewMetricImplementationService(repo *repository.MetricImplementationRepository, tableRepo *repository.LogicalTableRepository) *MetricImplementationService {
	return &MetricImplementationService{repo: repo, tableRepo: tableRepo}
}
func (s *MetricImplementationService) SetStandardClient(client *commonClient.StandardClient) {
	s.standard = client
}

func (s *MetricImplementationService) List(factTableID, tenantID int64) ([]models.MetricImplementation, error) {
	table, err := s.tableRepo.GetByID(factTableID, tenantID)
	if err != nil {
		return nil, apperrors.NotFound("logical_table_not_found", i18n.MsgTableNotFound)
	}
	if table.TableType != "fact" {
		return nil, apperrors.Validation("fact_table_required", i18n.MsgValidationFailed)
	}
	return s.repo.ListByFactTable(factTableID, tenantID)
}

func (s *MetricImplementationService) Create(factTableID, tenantID, userID int64, req *models.CreateMetricImplementationRequest) (*models.MetricImplementationMutationResponse, error) {
	if err := validateMetricImplementationRequest(req); err != nil {
		return nil, err
	}
	if s.standard != nil {
		if _, err := s.standard.WithTenantID(uint(tenantID)).GetPublishedMetricDefinitionRevision(context.Background(), req.MetricDefinitionID, req.MetricDefinitionRevisionID); err != nil {
			return nil, standardReferenceError(err, "metric_definition_revision_not_found")
		}
	}
	item := metricImplementationFromRequest(factTableID, tenantID, userID, req)
	response := &models.MetricImplementationMutationResponse{Implementation: item}
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := lockStandardReferences(tx, tenantID, requiredStandardReference(models.StandardResourceMetric, req.MetricDefinitionID)); err != nil {
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
			return apperrors.Conflict("metric_implementation_state_conflict", i18n.MsgTableStateConflict)
		}
		if err := validateMetricImplementationLocalReferences(tx, factTableID, req); err != nil {
			return err
		}
		if err := repository.NewMetricImplementationRepository(tx).Create(&response.Implementation); err != nil {
			return modelResourceError(err, "metric_implementation", i18n.MsgMetricImplementationConflict)
		}
		response.Version, err = repository.AdvanceLogicalTableVersion(tx, factTableID, tenantID, req.Version)
		return err
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

func (s *MetricImplementationService) Update(id, factTableID, tenantID, userID int64, req *models.UpdateMetricImplementationRequest) (*models.MetricImplementationMutationResponse, error) {
	if err := validateMetricImplementationRequest(req); err != nil {
		return nil, err
	}
	if s.standard != nil {
		if _, err := s.standard.WithTenantID(uint(tenantID)).GetPublishedMetricDefinitionRevision(context.Background(), req.MetricDefinitionID, req.MetricDefinitionRevisionID); err != nil {
			return nil, standardReferenceError(err, "metric_definition_revision_not_found")
		}
	}
	response := &models.MetricImplementationMutationResponse{}
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := lockStandardReferences(tx, tenantID, requiredStandardReference(models.StandardResourceMetric, req.MetricDefinitionID)); err != nil {
			return err
		}
		table, err := repository.LockLogicalTable(tx, factTableID, tenantID)
		if err != nil {
			return apperrors.NotFound("logical_table_not_found", i18n.MsgTableNotFound)
		}
		if err := requireVersion(table.Version, req.Version); err != nil {
			return err
		}
		if table.TableType != "fact" || table.Status != "draft" {
			return apperrors.Conflict("metric_implementation_state_conflict", i18n.MsgTableStateConflict)
		}
		txRepo := repository.NewMetricImplementationRepository(tx)
		existing, err := txRepo.GetByID(id, factTableID, tenantID)
		if err != nil {
			return apperrors.NotFound("metric_implementation_not_found", i18n.MsgInvalidMetricImplementationID)
		}
		if err := validateMetricImplementationLocalReferences(tx, factTableID, req); err != nil {
			return err
		}
		updated := metricImplementationFromRequest(factTableID, tenantID, userID, req)
		updated.ID, updated.CreatedBy, updated.CreatedAt = id, existing.CreatedBy, existing.CreatedAt
		updated.UpdatedBy = &userID
		if err := txRepo.Update(&updated); err != nil {
			return modelResourceError(err, "metric_implementation", i18n.MsgMetricImplementationConflict)
		}
		response.Implementation = updated
		response.Version, err = repository.AdvanceLogicalTableVersion(tx, factTableID, tenantID, req.Version)
		return err
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

func (s *MetricImplementationService) Delete(id, factTableID, tenantID, version int64) (*models.VersionResponse, error) {
	response := &models.VersionResponse{}
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		table, err := repository.LockLogicalTable(tx, factTableID, tenantID)
		if err != nil {
			return apperrors.NotFound("logical_table_not_found", i18n.MsgTableNotFound)
		}
		if err := requireVersion(table.Version, version); err != nil {
			return err
		}
		if table.TableType != "fact" || table.Status != "draft" {
			return apperrors.Conflict("metric_implementation_state_conflict", i18n.MsgTableStateConflict)
		}
		if err := repository.NewMetricImplementationRepository(tx).Delete(id, factTableID, tenantID); err != nil {
			return apperrors.NotFound("metric_implementation_not_found", i18n.MsgInvalidMetricImplementationID)
		}
		response.Version, err = repository.AdvanceLogicalTableVersion(tx, factTableID, tenantID, version)
		return err
	})
	return response, err
}

func metricImplementationFromRequest(factTableID, tenantID, userID int64, req *models.CreateMetricImplementationRequest) models.MetricImplementation {
	return models.MetricImplementation{TenantID: tenantID, FactTableID: factTableID, MetricDefinitionID: req.MetricDefinitionID, MetricDefinitionRevisionID: req.MetricDefinitionRevisionID, Name: strings.TrimSpace(req.Name), Grain: strings.TrimSpace(req.Grain), SourceConfig: models.JSONB(req.SourceConfig), DimensionConfig: models.JSONB(req.DimensionConfig), FilterConfig: models.JSONB(req.FilterConfig), ExpressionConfig: models.JSONB(req.ExpressionConfig), Status: req.Status, Note: strings.TrimSpace(req.Note), CreatedBy: userID}
}

func validateMetricImplementationRequest(req *models.CreateMetricImplementationRequest) error {
	if req == nil || req.Version <= 0 || req.MetricDefinitionID <= 0 || req.MetricDefinitionRevisionID <= 0 || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Grain) == "" || len(req.SourceConfig) == 0 || len(req.ExpressionConfig) == 0 || req.DimensionConfig == nil || req.FilterConfig == nil || (req.Status != models.MetricImplementationActive && req.Status != models.MetricImplementationDisabled) {
		return apperrors.Validation("invalid_metric_implementation", i18n.MsgValidationFailed)
	}
	if _, ok := req.SourceConfig["field_ids"]; !ok {
		return apperrors.Validation("invalid_metric_implementation", i18n.MsgValidationFailed)
	}
	if strings.TrimSpace(stringConfig(req.ExpressionConfig, "engine")) == "" || strings.TrimSpace(stringConfig(req.ExpressionConfig, "expression")) == "" {
		return apperrors.Validation("invalid_metric_implementation", i18n.MsgValidationFailed)
	}
	return nil
}
func validateMetricImplementationLocalReferences(tx *gorm.DB, factTableID int64, req *models.CreateMetricImplementationRequest) error {
	fieldIDs, err := positiveIDList(req.SourceConfig["field_ids"])
	if err != nil || len(fieldIDs) == 0 {
		return apperrors.Validation("invalid_metric_implementation", i18n.MsgValidationFailed)
	}
	tableRepo := repository.NewLogicalTableRepository(tx)
	for _, id := range fieldIDs {
		if _, err := tableRepo.GetFieldByID(id, factTableID); err != nil {
			return apperrors.NotFound("logical_field_not_found", i18n.MsgFieldNotFound)
		}
	}
	return nil
}
func positiveIDList(value interface{}) ([]int64, error) {
	var values []interface{}
	switch typed := value.(type) {
	case []interface{}:
		values = typed
	case []int64:
		values = make([]interface{}, len(typed))
		for index, id := range typed {
			values[index] = id
		}
	case []int:
		values = make([]interface{}, len(typed))
		for index, id := range typed {
			values[index] = id
		}
	default:
		return nil, fmt.Errorf("ID list required")
	}
	result := make([]int64, 0, len(values))
	seen := map[int64]struct{}{}
	for _, raw := range values {
		var id int64
		switch number := raw.(type) {
		case float64:
			id = int64(number)
			if number != float64(id) {
				return nil, fmt.Errorf("invalid ID")
			}
		case int64:
			id = number
		case int:
			id = int64(number)
		default:
			return nil, fmt.Errorf("invalid ID")
		}
		if id <= 0 {
			return nil, fmt.Errorf("invalid ID")
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("duplicate ID")
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result, nil
}
func stringConfig(value map[string]interface{}, key string) string {
	result, _ := value[key].(string)
	return result
}
