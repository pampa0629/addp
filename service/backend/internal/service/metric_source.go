package service

import (
	"context"
	"fmt"
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/selection"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/service/internal/models"
	"reflect"
	"time"
)

func analyticalEngine(engine *commonmodels.Engine) (plugin.AnalyticalInstance, plugin.AnalyticalCompiler, error) {
	if engine == nil || !selection.IsAvailableForAnalytical(engine) {
		return plugin.AnalyticalInstance{}, nil, plugin.ErrAnalyticalUnsupported
	}
	caps, err := selection.ParseCapabilities(engine.Capabilities)
	if err != nil {
		return plugin.AnalyticalInstance{}, nil, err
	}
	compiler, err := plugin.ResolveAnalyticalCompiler(engine.EngineType)
	if err != nil {
		return plugin.AnalyticalInstance{}, nil, err
	}
	return plugin.AnalyticalInstance{EngineID: engine.ID, Capability: *caps.Compute.Query.Analytical}, compiler, nil
}

func (s *QueryServiceService) resolveMetricSource(ctx context.Context, req *models.CreateQueryServiceRequest, tenantID uint) (*models.MetricSourceSnapshot, error) {
	if req.MetricSource == nil {
		if req.ConfigType == "analytical" {
			return nil, ErrInvalidStructuredQuery
		}
		return nil, nil
	}
	if s.modelClient == nil || s.systemClient == nil {
		return nil, fmt.Errorf("%w: metric plan provider unavailable", ErrInvalidStructuredQuery)
	}
	if req.ConfigType != "analytical" || req.SqlQuery != "" || req.EngineID != nil || req.RuntimeEngineID != nil || req.OutputContract != nil || len(req.NamedParameters) > 0 || len(req.DataConfig) > 0 || req.SchemaName != "" || req.TableName != "" {
		return nil, fmt.Errorf("%w: metric source cannot override frozen configuration", ErrInvalidStructuredQuery)
	}
	source := req.MetricSource
	frozen, err := s.modelClient.WithTenantID(tenantID).GetMetricPlan(ctx, source.ImplementationID, source.RevisionID, nil, source.ResultKind)
	if err != nil {
		return nil, err
	}
	engine, err := s.systemClient.GetEngineForTenant(ctx, tenantID, frozen.ExecutionPlan.EngineID)
	if err != nil {
		return nil, err
	}
	instance, compiler, err := analyticalEngine(engine)
	if err != nil {
		return nil, err
	}
	if err := frozen.ExecutionPlan.Verify(instance, compiler); err != nil {
		return nil, err
	}
	return frozen, nil
}

// The package is the sole source for analytical engine, parameters and output.
func validateAnalyticalPublication(service *models.QueryService) error {
	if service == nil || service.ConfigType != "analytical" || service.EngineID != nil || service.RuntimeEngineID != nil || service.SqlQuery != "" || service.SchemaName != "" || service.TargetTable != "" || len(service.NamedParameters) > 0 {
		return ErrInvalidStructuredQuery
	}
	snapshot := service.SourceSnapshot()
	if snapshot == nil || snapshot.MetricSource == nil || snapshot.Source != nil || snapshot.Table != nil || snapshot.Spatial != nil || snapshot.ObjectTable != nil || snapshot.QueryHash != "" || len(snapshot.FederatedSourceEngineIDs) > 0 || len(snapshot.FederatedObjectTables) > 0 {
		return ErrInvalidStructuredQuery
	}
	if _, exists := service.DataConfig["stable_key"]; exists {
		return ErrInvalidStructuredQuery
	}
	if snapshot.DependencyHash == "" || snapshot.DependencyHash != queryServiceDependencyHash(snapshot) {
		return ErrInvalidStructuredQuery
	}
	return snapshot.MetricSource.Validate()
}

func (s *QueryExecutorService) validateMetricSource(ctx context.Context, service *models.QueryService, input map[string]interface{}) error {
	snapshot := service.SourceSnapshot()
	if service.ConfigType != "analytical" {
		if snapshot != nil && snapshot.MetricSource != nil {
			return ErrInvalidStructuredQuery
		}
		return nil
	}
	if err := validateAnalyticalPublication(service); err != nil {
		return err
	}
	source := snapshot.MetricSource
	if s.modelClient == nil || len(input) != len(source.Parameters()) {
		return fmt.Errorf("%w: metric query requires exactly the declared parameters", ErrInvalidStructuredQuery)
	}
	for _, parameter := range source.Parameters() {
		value, ok := input[parameter.Name]
		if !ok {
			return fmt.Errorf("%w: missing metric parameter", ErrInvalidStructuredQuery)
		}
		if _, err := analyticalLiteral(parameter.Type, value); err != nil {
			return fmt.Errorf("%w: invalid metric parameter", ErrInvalidStructuredQuery)
		}
	}

	current, err := s.modelClient.WithTenantID(service.TenantID).GetMetricPlan(ctx, source.ImplementationID, source.RevisionID, input, source.ResultKind)
	if err != nil {
		return err
	}
	if current.DependencyHash != source.DependencyHash || current.MetricDefinitionID != source.MetricDefinitionID || current.MetricDefinitionRevisionID != source.MetricDefinitionRevisionID || current.ExecutionPlan.PackageHash != source.ExecutionPlan.PackageHash || !reflect.DeepEqual(current.ParameterLabels, source.ParameterLabels) || !reflect.DeepEqual(current.ParameterPresentation, source.ParameterPresentation) {
		return fmt.Errorf("%w: metric publication dependency changed", ErrInvalidStructuredQuery)
	}
	return nil
}

func (s *QueryServiceService) RebindMetricSource(ctx context.Context, id, tenantID uint, req *models.RebindMetricSourceRequest) (*models.QueryServiceDTO, error) {
	if req == nil || req.MetricSource == nil || req.Version <= 0 || tenantID == 0 {
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
	if current.Version != req.Version {
		return nil, commonapi.ErrConflict
	}
	compiled := &models.CreateQueryServiceRequest{ConfigType: "analytical", MetricSource: req.MetricSource}
	binding, err := s.resolveMetricSource(ctx, compiled, tenantID)
	if err != nil {
		return nil, err
	}
	snapshot := &models.QueryServiceDependencySnapshot{CapturedAt: time.Now(), MetricSource: binding}
	snapshot.DependencyHash = queryServiceDependencyHash(snapshot)
	config := models.JSONB{models.QueryServiceSourceSnapshotKey: queryServiceSnapshotPayload(snapshot)}
	item, err := s.repo.UpdateVersioned(ctx, id, tenantID, req.Version, func(item *models.QueryService) error {
		item.ConfigType = "analytical"
		item.EngineID = nil
		item.RuntimeEngineID = nil
		item.SchemaName = ""
		item.TargetTable = ""
		item.SqlQuery = ""
		item.NamedParameters = []models.QueryServiceNamedParameter{}
		item.DataConfig = config
		item.Protocols = s.buildProtocolsConfig(item.Protocols, false)
		// Preserve existing REST settings while removing formats unsupported by scalar output.
		if rest, ok := item.Protocols["rest_api"].(map[string]interface{}); ok {
			rest["formats"] = consumerRESTFormats(item, false)
		}
		candidate := *item
		candidate.Status = "active"
		if err := ValidateQueryConsumerContract(&candidate); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.convertToDTO(item), nil
}
