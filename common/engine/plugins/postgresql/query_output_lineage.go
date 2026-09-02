package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	pgquery "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func (p *PostgreSQLPlugin) resolvePreparedQueryOutputLineage(
	ctx context.Context,
	connInfo plugin.ConnectionInfo,
	req plugin.QueryRequest,
	readSet *plugin.QueryReadSet,
) (*plugin.QueryOutputLineage, error) {
	if readSet == nil {
		return nil, fmt.Errorf("%w: PostgreSQL read set is required", plugin.ErrQueryOutputLineageUnresolved)
	}
	sources, err := p.postgresLineageSources(ctx, connInfo, readSet)
	if err != nil {
		return nil, err
	}
	parsed, err := pgquery.Parse(strings.TrimSpace(req.Query))
	if err != nil || len(parsed.GetStmts()) != 1 {
		return nil, fmt.Errorf("%w: PostgreSQL query must contain exactly one statement", plugin.ErrQueryOutputLineageUnresolved)
	}
	statement := parsed.GetStmts()[0].GetStmt().GetSelectStmt()
	if statement == nil {
		markPostgresLineageOpaque(sources)
		return &plugin.QueryOutputLineage{Sources: sources}, nil
	}

	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("%w: build PostgreSQL lineage connection: %v", plugin.ErrQueryOutputLineageUnresolved, err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: open PostgreSQL lineage connection: %v", plugin.ErrQueryOutputLineageUnresolved, err)
	}
	defer db.Close() //nolint:errcheck
	resolvedSources, err := resolvePostgresSelectOutputLineage(ctx, &postgresDatabaseReadCatalog{db: db}, statement, sources)
	if err != nil {
		return nil, err
	}
	return &plugin.QueryOutputLineage{Sources: resolvedSources}, nil
}

func resolvePostgresSelectOutputLineage(
	ctx context.Context,
	catalog postgresReadCatalog,
	statement *pgquery.SelectStmt,
	sources []plugin.QueryOutputSource,
) ([]plugin.QueryOutputSource, error) {
	if statement == nil || statement.GetWithClause() != nil || statement.GetOp() != pgquery.SetOperation_SETOP_NONE || len(statement.GetFromClause()) != 1 {
		markPostgresLineageOpaque(sources)
		return sources, nil
	}
	from := statement.GetFromClause()[0]
	if rangeVar := from.GetRangeVar(); rangeVar != nil {
		return resolvePostgresRangeOutputLineage(ctx, catalog, statement, rangeVar, sources)
	}
	rangeSubselect := from.GetRangeSubselect()
	if rangeSubselect == nil || rangeSubselect.GetSubquery() == nil || strings.TrimSpace(rangeSubselect.GetAlias().GetAliasname()) == "" {
		markPostgresLineageOpaque(sources)
		return sources, nil
	}
	innerStatement := rangeSubselect.GetSubquery().GetSelectStmt()
	if innerStatement == nil {
		markPostgresLineageOpaque(sources)
		return sources, nil
	}
	inner, err := resolvePostgresSelectOutputLineage(ctx, catalog, innerStatement, sources)
	if err != nil {
		return nil, err
	}
	return composePostgresSubqueryOutputLineage(statement, rangeSubselect.GetAlias().GetAliasname(), inner), nil
}

func resolvePostgresRangeOutputLineage(
	ctx context.Context,
	catalog postgresReadCatalog,
	statement *pgquery.SelectStmt,
	rangeVar *pgquery.RangeVar,
	sources []plugin.QueryOutputSource,
) ([]plugin.QueryOutputSource, error) {
	resolved, err := catalog.ResolveRelation(ctx, postgresRelationReference{
		Schema: strings.TrimSpace(rangeVar.GetSchemaname()), Name: strings.TrimSpace(rangeVar.GetRelname()),
	})
	if err != nil {
		return nil, fmt.Errorf("%w: resolve PostgreSQL output relation: %v", plugin.ErrQueryOutputLineageUnresolved, err)
	}
	primary := postgresLineageSourceIndex(sources, resolved.Schema, resolved.Name)
	if primary < 0 {
		return nil, fmt.Errorf("%w: output relation is missing from read set", plugin.ErrQueryOutputLineageUnresolved)
	}
	for index := range sources {
		if index != primary {
			sources[index].OpaqueOutput = true
		}
	}
	qualifiers := map[string]struct{}{strings.TrimSpace(rangeVar.GetRelname()): {}}
	if alias := strings.TrimSpace(rangeVar.GetAlias().GetAliasname()); alias != "" {
		qualifiers[alias] = struct{}{}
	}
	if schema := strings.TrimSpace(rangeVar.GetSchemaname()); schema != "" {
		qualifiers[schema+"."+strings.TrimSpace(rangeVar.GetRelname())] = struct{}{}
	}

	if !applyPostgresRangeTargets(statement, qualifiers, &sources[primary]) {
		sources[primary].OpaqueOutput = true
		sources[primary].IdentityOutput = false
		sources[primary].Bindings = nil
	}
	return sources, nil
}

func applyPostgresRangeTargets(statement *pgquery.SelectStmt, qualifiers map[string]struct{}, source *plugin.QueryOutputSource) bool {
	for _, targetNode := range statement.GetTargetList() {
		target := targetNode.GetResTarget()
		if target == nil || target.GetVal() == nil {
			return false
		}
		if reference := target.GetVal().GetColumnRef(); reference != nil {
			column, wildcard, ok := postgresOutputColumn(reference, qualifiers)
			if !ok {
				return false
			}
			if wildcard {
				source.IdentityOutput = true
				continue
			}
			output := strings.TrimSpace(target.GetName())
			if output == "" {
				output = column
			}
			source.Bindings = append(source.Bindings, plugin.QueryOutputBinding{
				SourcePath: []string{column}, OutputPath: []string{output}, Transformation: plugin.QueryOutputTransformationDirect,
			})
			continue
		}
		for _, reference := range collectPostgresOutputColumnRefs(target.GetVal()) {
			column, wildcard, ok := postgresOutputColumn(reference, qualifiers)
			if !ok || wildcard {
				return false
			}
			source.Bindings = append(source.Bindings, plugin.QueryOutputBinding{
				SourcePath: []string{column}, OutputPath: postgresOptionalOutputPath(target.GetName()), Transformation: plugin.QueryOutputTransformationDerived,
			})
		}
	}
	return true
}

func composePostgresSubqueryOutputLineage(statement *pgquery.SelectStmt, alias string, inner []plugin.QueryOutputSource) []plugin.QueryOutputSource {
	result := make([]plugin.QueryOutputSource, len(inner))
	for index := range inner {
		result[index] = plugin.QueryOutputSource{Path: inner[index].Path, Fields: inner[index].Fields, OpaqueOutput: inner[index].OpaqueOutput}
	}
	qualifiers := map[string]struct{}{strings.TrimSpace(alias): {}}
	for _, targetNode := range statement.GetTargetList() {
		target := targetNode.GetResTarget()
		if target == nil || target.GetVal() == nil {
			markPostgresLineageOpaque(result)
			return result
		}
		if reference := target.GetVal().GetColumnRef(); reference != nil {
			column, wildcard, ok := postgresOutputColumn(reference, qualifiers)
			if !ok {
				markPostgresLineageOpaque(result)
				return result
			}
			if wildcard {
				for index := range inner {
					if inner[index].OpaqueOutput {
						continue
					}
					result[index].IdentityOutput = inner[index].IdentityOutput
					result[index].Bindings = append(result[index].Bindings, inner[index].Bindings...)
				}
				continue
			}
			output := strings.TrimSpace(target.GetName())
			if output == "" {
				output = column
			}
			if !appendPostgresComposedBinding(result, inner, column, []string{output}, plugin.QueryOutputTransformationDirect) {
				markPostgresLineageOpaque(result)
				return result
			}
			continue
		}
		for _, reference := range collectPostgresOutputColumnRefs(target.GetVal()) {
			column, wildcard, ok := postgresOutputColumn(reference, qualifiers)
			if !ok || wildcard || !appendPostgresComposedBinding(result, inner, column, postgresOptionalOutputPath(target.GetName()), plugin.QueryOutputTransformationDerived) {
				markPostgresLineageOpaque(result)
				return result
			}
		}
	}
	return result
}

func appendPostgresComposedBinding(
	result []plugin.QueryOutputSource,
	inner []plugin.QueryOutputSource,
	innerOutput string,
	outerOutput []string,
	outerTransformation string,
) bool {
	matched := false
	for index, source := range inner {
		if source.OpaqueOutput {
			continue
		}
		if source.IdentityOutput {
			result[index].Bindings = append(result[index].Bindings, plugin.QueryOutputBinding{
				SourcePath: []string{innerOutput}, OutputPath: outerOutput, Transformation: outerTransformation,
			})
			matched = true
		}
		for _, binding := range source.Bindings {
			if len(binding.OutputPath) != 1 || binding.OutputPath[0] != innerOutput {
				continue
			}
			transformation := outerTransformation
			if binding.Transformation != plugin.QueryOutputTransformationDirect {
				transformation = plugin.QueryOutputTransformationDerived
			}
			result[index].Bindings = append(result[index].Bindings, plugin.QueryOutputBinding{
				SourcePath: append([]string(nil), binding.SourcePath...), OutputPath: append([]string(nil), outerOutput...), Transformation: transformation,
			})
			matched = true
		}
	}
	return matched
}

func (p *PostgreSQLPlugin) postgresLineageSources(ctx context.Context, connInfo plugin.ConnectionInfo, readSet *plugin.QueryReadSet) ([]plugin.QueryOutputSource, error) {
	sources := make([]plugin.QueryOutputSource, 0, len(readSet.Paths))
	for _, path := range readSet.Paths {
		facts, err := p.DescribeEngineCatalogFacts(ctx, connInfo, path, plugin.EngineCatalogFactsOptions{})
		if err != nil || facts == nil || facts.Table == nil || len(facts.Table.Fields) == 0 {
			return nil, fmt.Errorf("%w: read PostgreSQL source fields", plugin.ErrQueryOutputLineageUnresolved)
		}
		sources = append(sources, plugin.QueryOutputSource{Path: path, Fields: facts.Table.Fields})
	}
	return sources, nil
}

func markPostgresLineageOpaque(sources []plugin.QueryOutputSource) {
	for index := range sources {
		sources[index].OpaqueOutput = true
	}
}

func postgresLineageSourceIndex(sources []plugin.QueryOutputSource, schema, relation string) int {
	for index, source := range sources {
		segments := plugin.EngineCatalogPathWithoutRoot(source.Path).Segments
		if len(segments) == 2 && segments[0].Name == schema && segments[1].Name == relation {
			return index
		}
	}
	return -1
}

func postgresOutputColumn(reference *pgquery.ColumnRef, qualifiers map[string]struct{}) (string, bool, bool) {
	parts := make([]string, 0, len(reference.GetFields()))
	wildcard := false
	for _, field := range reference.GetFields() {
		if field.GetAStar() != nil {
			wildcard = true
			parts = append(parts, "*")
			continue
		}
		value := strings.TrimSpace(field.GetString_().GetSval())
		if value == "" {
			return "", false, false
		}
		parts = append(parts, value)
	}
	if len(parts) == 1 {
		return parts[0], wildcard, true
	}
	qualifier := strings.Join(parts[:len(parts)-1], ".")
	if _, exists := qualifiers[qualifier]; !exists {
		return "", false, false
	}
	return parts[len(parts)-1], wildcard, true
}

func postgresOptionalOutputPath(name string) []string {
	if name = strings.TrimSpace(name); name != "" {
		return []string{name}
	}
	return nil
}

func collectPostgresOutputColumnRefs(node *pgquery.Node) []*pgquery.ColumnRef {
	if node == nil {
		return nil
	}
	if reference := node.GetColumnRef(); reference != nil {
		return []*pgquery.ColumnRef{reference}
	}
	return collectPostgresColumnRefsFromMessage(node.ProtoReflect())
}

func collectPostgresColumnRefsFromMessage(message protoreflect.Message) []*pgquery.ColumnRef {
	if !message.IsValid() {
		return nil
	}
	result := []*pgquery.ColumnRef{}
	fields := message.Descriptor().Fields()
	for index := 0; index < fields.Len(); index++ {
		field := fields.Get(index)
		if !message.Has(field) || field.Kind() != protoreflect.MessageKind {
			continue
		}
		value := message.Get(field)
		if field.IsList() {
			list := value.List()
			for itemIndex := 0; itemIndex < list.Len(); itemIndex++ {
				result = append(result, collectPostgresColumnRefsFromReflectedMessage(list.Get(itemIndex).Message())...)
			}
			continue
		}
		result = append(result, collectPostgresColumnRefsFromReflectedMessage(value.Message())...)
	}
	return result
}

func collectPostgresColumnRefsFromReflectedMessage(message protoreflect.Message) []*pgquery.ColumnRef {
	if node, ok := message.Interface().(*pgquery.Node); ok {
		return collectPostgresOutputColumnRefs(node)
	}
	return collectPostgresColumnRefsFromMessage(message)
}
