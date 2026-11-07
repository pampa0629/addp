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
	pgquery "github.com/pganalyze/pg_query_go/v6"
)

// queryRoleSource is request-local Provider evidence, never a Meta source snapshot.
type queryRoleSource struct {
	targets  map[dataprotection.ResourceReference]map[string][]string
	outputs  map[string]bool
	engine   plugin.EnginePlugin
	prepared plugin.PreparedQuery
}

func tableRoleSource(service *models.QueryService) (*queryRoleSource, error) {
	snapshot := service.SourceSnapshot()
	if service.ConfigType != "table" || service.UsesFederatedQueryRuntime() || snapshot == nil || snapshot.Source == nil || snapshot.ObjectTable != nil || snapshot.Table == nil {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	target := dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: snapshot.Source.ItemFingerprint}
	fields := map[string][]string{}
	for _, field := range snapshot.Table.Fields {
		fields[field.Name] = nil
	}
	return &queryRoleSource{targets: map[dataprotection.ResourceReference]map[string][]string{target: fields}}, nil
}

func sqlRoleSource(ctx context.Context, engine plugin.EnginePlugin, prepared plugin.PreparedQuery, engineID uint, baseSQL string) (*queryRoleSource, error) {
	model, ok := engine.(plugin.EngineCatalogModelProvider)
	if !ok || prepared == nil || engineID == 0 {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	readSet, err := prepared.ReadSet(ctx)
	if err != nil {
		return nil, errors.Join(dataprotection.ErrRoleOptionUnavailable, err)
	}
	if readSet == nil || len(readSet.Paths) == 0 || len(readSet.Paths) > 2 {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	joined := len(readSet.Paths) == 2
	if joined && (engine.Type() != "postgresql" || !isRoleMaskingJoin(baseSQL, readSet.Paths)) {
		return nil, dataprotection.ErrRoleOptionUnavailable
	}
	lineage, err := prepared.OutputLineage(ctx)
	if err != nil {
		return nil, errors.Join(dataprotection.ErrRoleOptionUnavailable, err)
	}
	if err := plugin.ValidateQueryOutputLineage(readSet, lineage); err != nil {
		return nil, errors.Join(dataprotection.ErrRoleOptionUnavailable, err)
	}
	source := &queryRoleSource{
		targets: map[dataprotection.ResourceReference]map[string][]string{},
		outputs: map[string]bool{}, engine: engine, prepared: prepared,
	}
	for _, providerSource := range lineage.Sources {
		path := providerSource.Path
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
		if _, duplicate := source.targets[target]; duplicate || providerSource.OpaqueOutput ||
			joined && providerSource.IdentityOutput || !joined && !providerSource.IdentityOutput && len(providerSource.Bindings) == 0 {
			return nil, dataprotection.ErrRoleOptionUnavailable
		}
		available := map[string]bool{}
		for _, field := range providerSource.Fields {
			if field.Name == "" || available[field.Name] || len(field.Path) > 1 || len(field.Path) == 1 && field.Path[0] != field.Name {
				return nil, dataprotection.ErrRoleOptionUnavailable
			}
			available[field.Name] = true
		}
		fields := map[string][]string{}
		source.targets[target] = fields
		if providerSource.IdentityOutput {
			for name := range available {
				source.outputs[name] = true
				fields[name] = []string{name}
			}
		}
		for _, binding := range providerSource.Bindings {
			if binding.Transformation != plugin.QueryOutputTransformationDirect || len(binding.SourcePath) != 1 || len(binding.OutputPath) != 1 {
				return nil, dataprotection.ErrRoleOptionUnavailable
			}
			field, output := binding.SourcePath[0], binding.OutputPath[0]
			if !available[field] || source.outputs[output] {
				return nil, dataprotection.ErrRoleOptionUnavailable
			}
			source.outputs[output] = true
			fields[field] = append(fields[field], output)
		}
	}
	return source, nil
}

// This limits the admitted syntax. Field identity and output mappings always
// come from the Provider, including for the final paginated PreparedQuery.
func isRoleMaskingJoin(sql string, paths []plugin.EngineCatalogPath) bool {
	tree, err := pgquery.Parse(sql)
	if err != nil || len(tree.GetStmts()) != 1 {
		return false
	}
	query := tree.GetStmts()[0].GetStmt().GetSelectStmt()
	if len(query.GetFromClause()) != 1 || query.GetWithClause() != nil ||
		len(query.GetGroupClause()) != 0 || query.GetHavingClause() != nil {
		return false
	}
	join := query.GetFromClause()[0].GetJoinExpr()
	if join.GetQuals() == nil || join.GetIsNatural() || len(join.GetUsingClause()) != 0 {
		return false
	}
	left, right := join.GetLarg().GetRangeVar(), join.GetRarg().GetRangeVar()
	if left == nil || right == nil {
		return false
	}
	matches := func(table *pgquery.RangeVar, path plugin.EngineCatalogPath) bool {
		segments := path.Segments
		return len(segments) >= 2 && segments[len(segments)-1].Name == table.GetRelname() &&
			(table.GetSchemaname() == "" || segments[len(segments)-2].Name == table.GetSchemaname())
	}
	if !(matches(left, paths[0]) && matches(right, paths[1]) || matches(left, paths[1]) && matches(right, paths[0])) {
		return false
	}
	for _, target := range query.GetTargetList() {
		fields := target.GetResTarget().GetVal().GetColumnRef().GetFields()
		if len(fields) == 0 || fields[len(fields)-1].GetAStar() != nil {
			return false
		}
	}
	return true
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
	return sqlRoleSource(ctx, enginePlugin, prepared, engineID, bound)
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
