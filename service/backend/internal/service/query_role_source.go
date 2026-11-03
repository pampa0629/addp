package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	"github.com/addp/service/internal/models"
)

// queryRoleSource is request-local Provider evidence, never a Meta source snapshot.
type queryRoleSource struct {
	target   dataprotection.ResourceReference
	fields   map[string]bool
	engine   plugin.EnginePlugin
	prepared plugin.PreparedQuery
}

func tableRoleSource(service *models.QueryService) (*queryRoleSource, error) {
	snapshot := service.SourceSnapshot()
	if service.ConfigType != "table" || service.UsesFederatedQueryRuntime() || snapshot == nil || snapshot.Source == nil || snapshot.ObjectTable != nil || snapshot.Table == nil {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	source := &queryRoleSource{target: dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: snapshot.Source.ItemFingerprint}, fields: map[string]bool{}}
	for _, field := range snapshot.Table.Fields {
		source.fields[field.Name] = true
	}
	return source, nil
}

func sqlRoleSource(ctx context.Context, engine plugin.EnginePlugin, prepared plugin.PreparedQuery, engineID uint) (*queryRoleSource, error) {
	model, ok := engine.(plugin.EngineCatalogModelProvider)
	if !ok || prepared == nil || engineID == 0 {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	readSet, err := prepared.ReadSet(ctx)
	if err != nil {
		return nil, errors.Join(dataprotection.ErrRoleOptionUnavailable, err)
	}
	if readSet == nil || len(readSet.Paths) != 1 {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	path := readSet.Paths[0]
	if path.EngineID != engineID || len(path.Segments) == 0 {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	leaf := path.Segments[len(path.Segments)-1]
	if leaf.Term != "table" || leaf.Kind != "table" {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	target, err := dataprotection.DataItemTargetFromCatalogPath(model.EngineCatalogModel(), path)
	if err != nil {
		return nil, errors.Join(dataprotection.ErrRoleOptionUnavailable, err)
	}
	lineage, err := prepared.OutputLineage(ctx)
	if err != nil {
		return nil, errors.Join(dataprotection.ErrRoleOptionUnavailable, err)
	}
	if err := plugin.ValidateQueryOutputLineage(readSet, lineage); err != nil {
		return nil, errors.Join(dataprotection.ErrRoleOptionUnavailable, err)
	}
	providerSource := lineage.Sources[0]
	if providerSource.OpaqueOutput || !providerSource.IdentityOutput && len(providerSource.Bindings) == 0 {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	for _, binding := range providerSource.Bindings {
		if binding.Transformation != plugin.QueryOutputTransformationDirect || len(binding.SourcePath) != 1 || len(binding.OutputPath) != 1 {
			return nil, dataprotection.ErrRoleOptionUnavailable
		}
	}
	source := &queryRoleSource{target: target, fields: map[string]bool{}, engine: engine, prepared: prepared}
	available := map[string]bool{}
	for _, field := range providerSource.Fields {
		if field.Name == "" || available[field.Name] || len(field.Path) > 1 || len(field.Path) == 1 && field.Path[0] != field.Name {
			return nil, dataprotection.ErrRoleOptionUnavailable
		}
		available[field.Name] = true
	}
	if providerSource.IdentityOutput {
		source.fields = available
	} else {
		for _, binding := range providerSource.Bindings {
			if !available[binding.SourcePath[0]] {
				return nil, dataprotection.ErrRoleOptionUnavailable
			}
			source.fields[binding.SourcePath[0]] = true
		}
	}
	return source, nil
}

// prepareRoleSQL only prepares Provider metadata; this path never executes the query.
func (s *QueryExecutorService) prepareRoleSQL(ctx context.Context, tenant, engineID uint, sql string, parameters map[string]any) (*queryRoleSource, error) {
	if s == nil || s.systemClient == nil || tenant == 0 || engineID == 0 || strings.TrimSpace(sql) == "" {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	if err := commonquery.RequireReadOnly(sql); err != nil {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	engine, err := s.systemClient.GetEngineForTenant(ctx, tenant, engineID)
	if err != nil {
		return nil, err
	}
	enginePlugin, err := plugin.Get(engine.EngineType)
	if err != nil {
		return nil, err
	}
	if _, federated := enginePlugin.(plugin.FederatedQueryRuntimeProvider); federated {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	provider, ok := enginePlugin.(plugin.QueryRuntimeProvider)
	if !ok {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	bound, args, err := commonquery.BindSQL(sql, parameters, commonquery.SQLPlaceholderStyleForDialect(engine.EngineType))
	if err != nil {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	prepared, err := provider.PrepareQuery(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), plugin.QueryRequest{EngineID: engineID, Language: "sql", Query: bound, Options: plugin.QueryOptions{EngineID: engineID, EngineType: engine.EngineType, ReadOnly: true, Timeout: time.Minute, Args: args}})
	if err != nil {
		return nil, err
	}
	return sqlRoleSource(ctx, enginePlugin, prepared, engineID)
}

func (s *QueryExecutorService) definitionRoleSource(ctx context.Context, service *models.QueryService) (*queryRoleSource, error) {
	if service.ConfigType != "sql" {
		return tableRoleSource(service)
	}
	if service.UsesFederatedQueryRuntime() {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	// Values are irrelevant to structural lineage. These NULL bindings are used
	// only for metadata preparation, never business execution or persisted defaults.
	references, err := commonquery.SQLReferences(service.SqlQuery)
	if err != nil {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	parameters := make(map[string]any, len(references))
	for _, name := range references {
		parameters[name] = nil
	}
	return s.prepareRoleSQL(ctx, service.TenantID, service.GetEngineID(), service.SqlQuery, parameters)
}
