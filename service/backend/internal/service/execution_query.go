package service

import (
	"context"
	"fmt"
	"strings"

	commonapi "github.com/addp/common/api"
	"github.com/addp/service/internal/models"
)

// GetExecutionQueryService resolves only a definition owned by this tenant.
func (s *QueryServiceService) GetExecutionQueryService(id, tenantID uint) (*models.QueryService, error) {
	if id == 0 || tenantID == 0 {
		return nil, commonapi.ErrNotFound
	}
	return s.repo.GetByIDAndTenant(id, tenantID)
}

// PreviewExecutionQuery compiles through the execution path without preparing
// or executing a database query. Inactive definitions may still be inspected.
func (s *QueryExecutorService) PreviewExecutionQuery(ctx context.Context, item *models.QueryService, request *models.QueryExecutionRequest) (*models.ExecutionQueryPreview, error) {
	if item == nil || request == nil || item.ConfigType != "analytical" {
		return nil, fmt.Errorf("%w: analytical source required", ErrInvalidStructuredQuery)
	}
	format := strings.ToLower(strings.TrimSpace(request.Format))
	if format != "" && !QueryServiceSupportsRESTFormat(item, format) {
		return nil, ErrInvalidStructuredQuery
	}
	if err := s.validateMetricSource(ctx, item, request.Parameters); err != nil {
		return nil, err
	}
	engine, err := s.systemClient.GetEngineForTenant(ctx, item.TenantID, item.GetEngineID())
	if err != nil {
		return nil, err
	}
	compiled, err := compileAnalyticalRequest(item, request, queryProtocolREST, engine, s.tokenCodec)
	if err != nil {
		return nil, err
	}
	source := item.MetricPlan()
	return &models.ExecutionQueryPreview{
		Language: compiled.query.Language, Query: compiled.query.Query, Parameters: compiled.parameters,
		EngineID: engine.ID, EngineName: engine.Name, EngineType: engine.EngineType,
		Version: item.Version, ImplementationID: source.ImplementationID, RevisionID: source.RevisionID, ResultKind: source.ResultKind,
	}, nil
}
