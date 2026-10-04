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

// A column carries ordered result identity and physical value origins through
// relation scopes. Row-selection dependencies never enter these origins.
type postgresValueOrigin struct {
	source  int
	path    []string
	derived bool
}
type postgresValueColumn struct {
	name    string
	origins []postgresValueOrigin
}
type postgresValueRelation struct {
	columns   []postgresValueColumn
	qualified map[string][]postgresValueColumn
}
type postgresOutputResolver struct {
	ctx           context.Context
	catalog       postgresReadCatalog
	sources       []plugin.QueryOutputSource
	opaqueSources map[int]bool
}

func resolvePostgresSelectOutputLineage(ctx context.Context, catalog postgresReadCatalog, statement *pgquery.SelectStmt, sources []plugin.QueryOutputSource) ([]plugin.QueryOutputSource, error) {
	resolver := postgresOutputResolver{ctx: ctx, catalog: catalog, sources: sources, opaqueSources: map[int]bool{}}
	columns, err := resolver.selectColumns(statement, nil)
	if err != nil {
		markPostgresLineageOpaque(sources)
		return sources, nil
	}
	for i := range sources {
		sources[i].Bindings = nil
		sources[i].IdentityOutput = false
		sources[i].OpaqueOutput = false
	}
	for _, column := range columns {
		for _, origin := range column.origins {
			transformation := plugin.QueryOutputTransformationDirect
			if origin.derived {
				transformation = plugin.QueryOutputTransformationDerived
			}
			sources[origin.source].Bindings = append(sources[origin.source].Bindings, plugin.QueryOutputBinding{SourcePath: append([]string(nil), origin.path...), OutputPath: []string{column.name}, Transformation: transformation})
		}
	}
	for index := range resolver.opaqueSources {
		sources[index].OpaqueOutput = true
		sources[index].IdentityOutput = false
		sources[index].Bindings = nil
	}
	return sources, nil
}

func (r *postgresOutputResolver) selectColumns(stmt *pgquery.SelectStmt, inherited map[string][]postgresValueColumn) ([]postgresValueColumn, error) {
	if stmt == nil || len(stmt.GetWindowClause()) > 0 {
		return nil, fmt.Errorf("missing SELECT")
	}
	scope := make(map[string][]postgresValueColumn, len(inherited))
	for name, columns := range inherited {
		scope[name] = columns
	}
	if with := stmt.GetWithClause(); with != nil {
		if with.GetRecursive() {
			return nil, fmt.Errorf("recursive CTE output is unresolved")
		}
		for _, node := range with.GetCtes() {
			cte := node.GetCommonTableExpr()
			if cte == nil {
				return nil, fmt.Errorf("invalid CTE")
			}
			columns, err := r.selectColumns(cte.GetCtequery().GetSelectStmt(), scope)
			if err != nil {
				return nil, err
			}
			columns, err = renamePostgresValueColumns(columns, cte.GetAliascolnames())
			if err != nil {
				return nil, err
			}
			scope[cte.GetCtename()] = columns
		}
	}
	if stmt.GetOp() != pgquery.SetOperation_SETOP_NONE {
		if stmt.GetOp() != pgquery.SetOperation_SETOP_UNION {
			return nil, fmt.Errorf("set output is unresolved")
		}
		left, err := r.selectColumns(stmt.GetLarg(), scope)
		if err != nil {
			return nil, err
		}
		right, err := r.selectColumns(stmt.GetRarg(), scope)
		if err != nil {
			return nil, err
		}
		if len(left) != len(right) {
			return nil, fmt.Errorf("set output arity differs")
		}
		columns := make([]postgresValueColumn, len(left))
		for i := range left {
			columns[i] = postgresValueColumn{name: left[i].name, origins: mergePostgresValueOrigins(left[i].origins, right[i].origins, true)}
		}
		return columns, checkPostgresValueColumnNames(columns)
	}
	relation := postgresValueRelation{qualified: map[string][]postgresValueColumn{}}
	for _, node := range stmt.GetFromClause() {
		next, err := r.fromRelation(node, scope)
		if err != nil {
			return nil, err
		}
		relation.columns = append(relation.columns, next.columns...)
		if err := mergePostgresValueQualifiers(relation.qualified, next.qualified); err != nil {
			return nil, err
		}
	}
	columns := []postgresValueColumn{}
	for _, node := range stmt.GetTargetList() {
		target := node.GetResTarget()
		if target == nil || target.GetVal() == nil {
			return nil, fmt.Errorf("invalid output target")
		}
		if ref := target.GetVal().GetColumnRef(); ref != nil {
			selected, err := postgresValueReference(ref, relation)
			if err != nil {
				return nil, err
			}
			for _, column := range selected {
				if target.GetName() != "" {
					column.name = target.GetName()
				}
				columns = append(columns, column)
			}
			continue
		}
		if postgresOutputHasUnresolvedScope(target.GetVal().ProtoReflect()) {
			return nil, fmt.Errorf("nested output scope is unresolved")
		}
		origins := []postgresValueOrigin{}
		for _, ref := range collectPostgresOutputColumnRefs(target.GetVal()) {
			selected, err := postgresValueReference(ref, relation)
			if err != nil || len(selected) != 1 {
				return nil, fmt.Errorf("expression source is unresolved")
			}
			origins = mergePostgresValueOrigins(origins, selected[0].origins, true)
		}
		name := target.GetName()
		if name == "" {
			name = postgresValueExpressionName(target.GetVal())
		}
		if name == "" {
			return nil, fmt.Errorf("expression output name is unresolved")
		}
		columns = append(columns, postgresValueColumn{name: name, origins: origins})
	}
	return columns, checkPostgresValueColumnNames(columns)
}

func (r *postgresOutputResolver) fromRelation(node *pgquery.Node, scope map[string][]postgresValueColumn) (postgresValueRelation, error) {
	if node == nil {
		return postgresValueRelation{}, fmt.Errorf("missing relation")
	}
	if table := node.GetRangeVar(); table != nil {
		var columns []postgresValueColumn
		if table.GetSchemaname() == "" {
			columns = scope[table.GetRelname()]
		}
		if columns == nil {
			resolved, err := r.catalog.ResolveRelation(r.ctx, postgresRelationReference{Schema: table.GetSchemaname(), Name: table.GetRelname()})
			if err != nil {
				return postgresValueRelation{}, err
			}
			index := postgresLineageSourceIndex(r.sources, resolved.Schema, resolved.Name)
			if index < 0 {
				return postgresValueRelation{}, fmt.Errorf("source outside read set")
			}
			if resolved.Relkind == "v" {
				for i := range r.sources {
					if i != index {
						r.opaqueSources[i] = true
					}
				}
			}
			for _, field := range r.sources[index].Fields {
				path := field.Path
				if len(path) == 0 {
					path = []string{field.Name}
				}
				columns = append(columns, postgresValueColumn{name: field.Name, origins: []postgresValueOrigin{{source: index, path: path}}})
			}
		}
		columns, err := renamePostgresValueColumns(columns, table.GetAlias().GetColnames())
		if err != nil {
			return postgresValueRelation{}, err
		}
		qualified := map[string][]postgresValueColumn{}
		if alias := table.GetAlias().GetAliasname(); alias != "" {
			qualified[alias] = columns
		} else {
			qualified[table.GetRelname()] = columns
			if table.GetSchemaname() != "" {
				qualified[table.GetSchemaname()+"."+table.GetRelname()] = columns
			}
		}
		return postgresValueRelation{columns: columns, qualified: qualified}, nil
	}
	if sub := node.GetRangeSubselect(); sub != nil {
		if sub.GetLateral() || sub.GetAlias().GetAliasname() == "" {
			return postgresValueRelation{}, fmt.Errorf("subquery scope is unresolved")
		}
		columns, err := r.selectColumns(sub.GetSubquery().GetSelectStmt(), scope)
		if err != nil {
			return postgresValueRelation{}, err
		}
		columns, err = renamePostgresValueColumns(columns, sub.GetAlias().GetColnames())
		if err != nil {
			return postgresValueRelation{}, err
		}
		return postgresValueRelation{columns: columns, qualified: map[string][]postgresValueColumn{sub.GetAlias().GetAliasname(): columns}}, nil
	}
	if join := node.GetJoinExpr(); join != nil {
		if join.GetIsNatural() {
			return postgresValueRelation{}, fmt.Errorf("natural join output is unresolved")
		}
		left, err := r.fromRelation(join.GetLarg(), scope)
		if err != nil {
			return postgresValueRelation{}, err
		}
		right, err := r.fromRelation(join.GetRarg(), scope)
		if err != nil {
			return postgresValueRelation{}, err
		}
		result := postgresValueRelation{qualified: map[string][]postgresValueColumn{}}
		if err := mergePostgresValueQualifiers(result.qualified, left.qualified); err != nil {
			return result, err
		}
		if err := mergePostgresValueQualifiers(result.qualified, right.qualified); err != nil {
			return result, err
		}
		using := map[string]bool{}
		for _, key := range join.GetUsingClause() {
			name := key.GetString_().GetSval()
			l, err := uniquePostgresValueColumn(left.columns, name)
			if err != nil {
				return result, err
			}
			rr, err := uniquePostgresValueColumn(right.columns, name)
			if err != nil {
				return result, err
			}
			switch join.GetJointype() {
			case pgquery.JoinType_JOIN_INNER, pgquery.JoinType_JOIN_LEFT:
			case pgquery.JoinType_JOIN_RIGHT:
				l = rr
			case pgquery.JoinType_JOIN_FULL:
				l.origins = mergePostgresValueOrigins(l.origins, rr.origins, true)
			default:
				return result, fmt.Errorf("join output is unresolved")
			}
			// The merged key can undergo PostgreSQL common-type coercion.
			// Preserve only the side(s) supplying its value, with derived semantics.
			l.origins = mergePostgresValueOrigins(l.origins, nil, true)
			result.columns = append(result.columns, l)
			using[name] = true
		}
		for _, side := range [][]postgresValueColumn{left.columns, right.columns} {
			for _, column := range side {
				if !using[column.name] {
					result.columns = append(result.columns, column)
				}
			}
		}
		if alias := join.GetAlias(); alias != nil {
			result.columns, err = renamePostgresValueColumns(result.columns, alias.GetColnames())
			if err != nil {
				return result, err
			}
			result.qualified = map[string][]postgresValueColumn{alias.GetAliasname(): result.columns}
		}
		// PostgreSQL's USING alias exposes only the merged key columns.
		if alias := join.GetJoinUsingAlias(); alias != nil {
			result.qualified[alias.GetAliasname()] = result.columns[:len(using)]
		}
		return result, nil
	}
	return postgresValueRelation{}, fmt.Errorf("relation output is unresolved")
}

func postgresValueReference(ref *pgquery.ColumnRef, relation postgresValueRelation) ([]postgresValueColumn, error) {
	parts := []string{}
	for _, field := range ref.GetFields() {
		if field.GetAStar() != nil {
			parts = append(parts, "*")
		} else if value := field.GetString_().GetSval(); value != "" {
			parts = append(parts, value)
		} else {
			return nil, fmt.Errorf("invalid field reference")
		}
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty field reference")
	}
	columns := relation.columns
	if len(parts) > 1 {
		var ok bool
		columns, ok = relation.qualified[strings.Join(parts[:len(parts)-1], ".")]
		if !ok {
			return nil, fmt.Errorf("unknown qualifier")
		}
	}
	name := parts[len(parts)-1]
	if name == "*" {
		if len(columns) == 0 {
			return nil, fmt.Errorf("empty wildcard")
		}
		return columns, nil
	}
	column, err := uniquePostgresValueColumn(columns, name)
	if err != nil {
		return nil, err
	}
	return []postgresValueColumn{column}, nil
}
func uniquePostgresValueColumn(columns []postgresValueColumn, name string) (postgresValueColumn, error) {
	found := -1
	for i, column := range columns {
		if column.name == name {
			if found >= 0 {
				return postgresValueColumn{}, fmt.Errorf("ambiguous field")
			}
			found = i
		}
	}
	if found < 0 {
		return postgresValueColumn{}, fmt.Errorf("unknown field")
	}
	return columns[found], nil
}
func mergePostgresValueOrigins(left, right []postgresValueOrigin, derived bool) []postgresValueOrigin {
	result := []postgresValueOrigin{}
	indexes := map[string]int{}
	for _, origins := range [][]postgresValueOrigin{left, right} {
		for _, origin := range origins {
			origin.derived = origin.derived || derived
			key := fmt.Sprintf("%d:%s", origin.source, strings.Join(origin.path, "\x00"))
			if index, exists := indexes[key]; exists {
				result[index].derived = result[index].derived || origin.derived
			} else {
				indexes[key] = len(result)
				result = append(result, origin)
			}
		}
	}
	return result
}
func mergePostgresValueQualifiers(target, source map[string][]postgresValueColumn) error {
	for key, columns := range source {
		if _, exists := target[key]; exists {
			return fmt.Errorf("duplicate qualifier")
		}
		target[key] = columns
	}
	return nil
}
func renamePostgresValueColumns(columns []postgresValueColumn, aliases []*pgquery.Node) ([]postgresValueColumn, error) {
	if len(aliases) > len(columns) {
		return nil, fmt.Errorf("too many column aliases")
	}
	result := append([]postgresValueColumn(nil), columns...)
	for i, alias := range aliases {
		result[i].name = alias.GetString_().GetSval()
	}
	return result, checkPostgresValueColumnNames(result)
}
func checkPostgresValueColumnNames(columns []postgresValueColumn) error {
	seen := map[string]bool{}
	for _, column := range columns {
		if column.name == "" || seen[column.name] {
			return fmt.Errorf("empty or duplicate output name")
		}
		seen[column.name] = true
	}
	return nil
}
func postgresValueExpressionName(node *pgquery.Node) string {
	if node.GetAConst() != nil || node.GetAExpr() != nil || node.GetBoolExpr() != nil {
		return "?column?"
	}
	if call := node.GetFuncCall(); call != nil {
		names := call.GetFuncname()
		if len(names) > 0 {
			return names[len(names)-1].GetString_().GetSval()
		}
	}
	if cast := node.GetTypeCast(); cast != nil {
		if ref := cast.GetArg().GetColumnRef(); ref != nil {
			parts := ref.GetFields()
			if len(parts) > 0 {
				return parts[len(parts)-1].GetString_().GetSval()
			}
		}
		return ""
	}

	if node.GetCoalesceExpr() != nil {
		return "coalesce"
	}
	return ""
}
func postgresOutputHasUnresolvedScope(message protoreflect.Message) bool {
	if node, ok := message.Interface().(*pgquery.Node); ok {
		if node.GetSubLink() != nil || node.GetFuncCall().GetOver() != nil || node.GetFuncCall().GetAggWithinGroup() {
			return true
		}
	}
	found := false
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.Kind() != protoreflect.MessageKind {
			return true
		}
		if field.IsList() {
			list := value.List()
			for i := 0; i < list.Len(); i++ {
				if postgresOutputHasUnresolvedScope(list.Get(i).Message()) {
					found = true
					break
				}
			}
		} else {
			found = postgresOutputHasUnresolvedScope(value.Message())
		}
		return !found
	})
	return found
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
		sources[index].IdentityOutput = false
		sources[index].Bindings = nil
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

func collectPostgresOutputColumnRefs(node *pgquery.Node) []*pgquery.ColumnRef {
	if node == nil {
		return nil
	}
	if reference := node.GetColumnRef(); reference != nil {
		return []*pgquery.ColumnRef{reference}
	}
	// Aggregate FILTER and ORDER BY select or order rows; only arguments
	// supply field values. Ordered-set aggregates are rejected separately.
	if call := node.GetFuncCall(); call != nil {
		result := []*pgquery.ColumnRef{}
		for _, arg := range call.GetArgs() {
			result = append(result, collectPostgresOutputColumnRefs(arg)...)
		}
		return result
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
