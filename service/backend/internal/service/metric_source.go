package service

import (
	"context"
	"fmt"
	commonapi "github.com/addp/common/api"
	"reflect"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/service/internal/models"
)

func (s *QueryServiceService) resolveMetricSource(ctx context.Context, req *models.CreateQueryServiceRequest, tenantID uint) (*models.MetricSourceSnapshot, error) {
	if req.MetricSource == nil {
		return nil, nil
	}
	if s.modelClient == nil {
		return nil, fmt.Errorf("%w: metric plan provider unavailable", ErrInvalidStructuredQuery)
	}
	if req.ConfigType != "sql" || req.SqlQuery != "" || req.EngineID != nil || req.RuntimeEngineID != nil || req.OutputContract != nil || len(req.NamedParameters) > 0 || len(req.DataConfig) > 0 {
		return nil, fmt.Errorf("%w: metric source cannot override compiled configuration", ErrInvalidStructuredQuery)
	}
	source := req.MetricSource
	plan, err := s.modelClient.WithTenantID(tenantID).GetMetricPlan(ctx, source.ImplementationID, source.RevisionID, nil)
	if err != nil {
		return nil, err
	}
	engine, err := s.systemClient.GetEngineForTenant(ctx, tenantID, plan.EngineID)
	if err != nil {
		return nil, err
	}
	if engine.EngineType != "postgresql" && engine.EngineType != "postgres" && engine.EngineType != "postgis" {
		return nil, fmt.Errorf("%w: metric plan requires PostgreSQL", ErrInvalidStructuredQuery)
	}
	req.SqlQuery = plan.SQL
	req.EngineID = &plan.EngineID
	req.NamedParameters = make([]models.QueryServiceNamedParameter, 0, len(plan.Parameters))
	for _, parameter := range plan.Parameters {
		req.NamedParameters = append(req.NamedParameters, models.QueryServiceNamedParameter{Name: parameter.Name, Type: parameter.Type, Required: parameter.Required, Options: parameter.Options})
	}
	req.DataConfig = map[string]interface{}{"stable_key": plan.StableKey}
	req.OutputContract = &models.QueryServiceOutputContract{Table: &datatype.TableInfo{Fields: plan.Fields}}
	return &models.MetricSourceSnapshot{ImplementationID: plan.ImplementationID, RevisionID: plan.RevisionID, MetricDefinitionID: plan.MetricDefinitionID, MetricDefinitionRevisionID: plan.MetricDefinitionRevisionID, DependencyHash: plan.DependencyHash}, nil
}
func (s *QueryExecutorService) validateMetricSource(ctx context.Context, service *models.QueryService, input map[string]interface{}) error {
	snapshot := service.SourceSnapshot()
	if snapshot == nil || snapshot.MetricSource == nil {
		return nil
	}
	source := snapshot.MetricSource
	if s.modelClient == nil || len(input) != len(service.NamedParameters) {
		return fmt.Errorf("%w: metric query requires exactly the declared parameters", ErrInvalidStructuredQuery)
	}
	for _, parameter := range service.NamedParameters {
		key := parameter.Name
		value, ok := input[key].(string)
		if !ok || value == "" {
			return fmt.Errorf("%w: invalid metric parameter", ErrInvalidStructuredQuery)
		}
	}
	plan, err := s.modelClient.WithTenantID(service.TenantID).GetMetricPlan(ctx, source.ImplementationID, source.RevisionID, input)
	if err != nil {
		return err
	}
	if len(plan.Parameters) != len(service.NamedParameters) || service.GetTableInfo() == nil || !reflect.DeepEqual(plan.Fields, service.GetTableInfo().Fields) || !reflect.DeepEqual(plan.StableKey, service.GetStableKey()) {
		return fmt.Errorf("%w: metric publication signature changed", ErrInvalidStructuredQuery)
	}
	for i, p := range plan.Parameters {
		declared := service.NamedParameters[i]
		if p.Name != declared.Name || p.Type != declared.Type || p.Required != declared.Required || !reflect.DeepEqual(p.Options, declared.Options) {
			return fmt.Errorf("%w: metric parameter signature changed", ErrInvalidStructuredQuery)
		}
	}
	if plan.DependencyHash != source.DependencyHash || plan.MetricDefinitionID != source.MetricDefinitionID || plan.MetricDefinitionRevisionID != source.MetricDefinitionRevisionID || plan.EngineID != service.GetEngineID() || plan.SQL != service.SqlQuery {
		return fmt.Errorf("%w: metric publication dependency changed", ErrInvalidStructuredQuery)
	}
	return nil
}

func (s *QueryServiceService) RebindMetricSource(ctx context.Context, id, tenantID uint, req *models.RebindMetricSourceRequest) (*models.QueryServiceDTO, error) {
	if req == nil || req.MetricSource == nil || req.ServiceVersion == "" || tenantID == 0 {
		return nil, ErrInvalidStructuredQuery
	}
	// Check ownership before resolving a new publication.
	current, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if current.TenantID != tenantID {
		return nil, commonapi.ErrNotFound
	}
	compiled := &models.CreateQueryServiceRequest{ConfigType: "sql", MetricSource: req.MetricSource}
	binding, err := s.resolveMetricSource(ctx, compiled, tenantID)
	if err != nil {
		return nil, err
	}
	if err := s.validateDirectSQLQueryEngine(ctx, tenantID, *compiled.EngineID); err != nil {
		return nil, err
	}
	compiled.NamedParameters, err = validateQueryServiceNamedParameters("sql", compiled.SqlQuery, compiled.NamedParameters)
	if err != nil {
		return nil, err
	}
	snapshot := buildSQLDependencySnapshot(compiled.SqlQuery, compiled.OutputContract, time.Now())
	snapshot.MetricSource = binding
	snapshot.DependencyHash = queryServiceDependencyHash(snapshot)
	config := models.JSONB{"stable_key": compiled.DataConfig["stable_key"], models.QueryServiceSourceSnapshotKey: queryServiceSnapshotPayload(snapshot)}
	item, err := s.repo.UpdatePublication(ctx, id, tenantID, func(item *models.QueryService) error {
		if QueryServiceVersion(item) != req.ServiceVersion {
			return commonapi.ErrConflict
		}
		if item.ConfigType != "sql" {
			return ErrInvalidStructuredQuery
		}
		item.EngineID = compiled.EngineID
		item.RuntimeEngineID = nil
		item.SchemaName = ""
		item.TargetTable = ""
		item.SqlQuery = compiled.SqlQuery
		item.NamedParameters = compiled.NamedParameters
		item.DataConfig = config
		item.Protocols = s.buildProtocolsConfig(item.Protocols, false)
		// Preserve existing REST settings while removing formats unsupported by scalar output.
		if rest, ok := item.Protocols["rest_api"].(map[string]interface{}); ok {
			rest["formats"] = consumerRESTFormats(item, false)
		}
		if err := ValidateQueryConsumerContract(item); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.convertToDTO(item), nil
}
