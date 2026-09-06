package shared

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/xwb1989/sqlparser"
)

type mysqlCompatibleLineageOrigin struct {
	sourceIndex int
	sourcePath  []string
}

type mysqlCompatibleLineageRelation struct {
	columns map[string]mysqlCompatibleLineageOrigin
}

type mysqlCompatibleLineageProjection struct {
	columns map[string]mysqlCompatibleLineageOrigin
	order   []string
}

type mysqlCompatibleLineageScope struct {
	relations   map[string]mysqlCompatibleLineageRelation
	unqualified map[string][]mysqlCompatibleLineageOrigin
}

// ResolveOutputLineage proves direct output-field lineage for a supported
// MySQL-compatible SELECT using current catalog facts from the calling plugin.
func (p MySQLCompatibleQueryProvenance) ResolveOutputLineage(
	ctx context.Context,
	connInfo plugin.ConnectionInfo,
	req plugin.QueryRequest,
	readSet *plugin.QueryReadSet,
) (*plugin.QueryOutputLineage, error) {
	if readSet == nil {
		return nil, p.outputLineageError("read set is required")
	}
	sources, err := p.lineageSources(ctx, connInfo, readSet)
	if err != nil {
		return nil, err
	}
	statement, err := sqlparser.Parse(strings.TrimSpace(req.Query))
	if err != nil {
		return nil, p.outputLineageError("query must contain exactly one supported SELECT")
	}
	defaultDatabase := strings.TrimSpace(plugin.ParseDriverConnInfo(connInfo, p.DefaultPort, "").Database)
	if defaultDatabase == "" {
		return nil, p.outputLineageError("current database is required")
	}
	resolved, err := p.resolveSelectOutputLineage(defaultDatabase, statement, sources)
	if err != nil {
		return nil, err
	}
	return &plugin.QueryOutputLineage{Sources: resolved}, nil
}

func (p MySQLCompatibleQueryProvenance) lineageSources(
	ctx context.Context,
	connInfo plugin.ConnectionInfo,
	readSet *plugin.QueryReadSet,
) ([]plugin.QueryOutputSource, error) {
	if p.DescribeFacts == nil {
		return nil, p.outputLineageError("catalog facts provider is required")
	}
	sources := make([]plugin.QueryOutputSource, 0, len(readSet.Paths))
	for _, path := range readSet.Paths {
		facts, err := p.DescribeFacts(ctx, connInfo, path, plugin.EngineCatalogFactsOptions{})
		if err != nil || facts == nil || facts.Table == nil || len(facts.Table.Fields) == 0 {
			return nil, p.outputLineageError("read current source fields")
		}
		sources = append(sources, plugin.QueryOutputSource{Path: path, Fields: facts.Table.Fields})
	}
	return sources, nil
}

func (p MySQLCompatibleQueryProvenance) resolveSelectOutputLineage(
	defaultDatabase string,
	statement sqlparser.Statement,
	sources []plugin.QueryOutputSource,
) ([]plugin.QueryOutputSource, error) {
	selectStatement, ok := statement.(sqlparser.SelectStatement)
	if !ok {
		return nil, p.outputLineageError("query is not a SELECT")
	}
	projection, err := p.resolveSelectProjection(defaultDatabase, selectStatement, sources)
	if err != nil {
		return nil, err
	}
	result := make([]plugin.QueryOutputSource, len(sources))
	for index, source := range sources {
		result[index] = plugin.QueryOutputSource{Path: source.Path, Fields: source.Fields}
	}
	for _, output := range projection.order {
		origin := projection.columns[output]
		if origin.sourceIndex < 0 || origin.sourceIndex >= len(result) {
			return nil, p.outputLineageError("projection source is outside the read set")
		}
		result[origin.sourceIndex].Bindings = append(result[origin.sourceIndex].Bindings, plugin.QueryOutputBinding{
			SourcePath:     append([]string(nil), origin.sourcePath...),
			OutputPath:     []string{output},
			Transformation: plugin.QueryOutputTransformationDirect,
		})
	}
	return result, nil
}

func (p MySQLCompatibleQueryProvenance) resolveSelectProjection(
	defaultDatabase string,
	statement sqlparser.SelectStatement,
	sources []plugin.QueryOutputSource,
) (*mysqlCompatibleLineageProjection, error) {
	selectNode, ok := statement.(*sqlparser.Select)
	if !ok {
		return nil, p.outputLineageError("UNION and parenthesized top-level queries are not supported")
	}
	scope, err := p.buildLineageScope(defaultDatabase, selectNode.From, sources)
	if err != nil {
		return nil, err
	}
	projection := &mysqlCompatibleLineageProjection{
		columns: make(map[string]mysqlCompatibleLineageOrigin, len(selectNode.SelectExprs)),
		order:   make([]string, 0, len(selectNode.SelectExprs)),
	}
	for _, expression := range selectNode.SelectExprs {
		aliased, ok := expression.(*sqlparser.AliasedExpr)
		if !ok {
			return nil, p.outputLineageError("wildcard and non-column projections are not supported")
		}
		column, ok := aliased.Expr.(*sqlparser.ColName)
		if !ok {
			return nil, p.outputLineageError("only direct column projections are supported")
		}
		origin, err := p.resolveLineageColumn(scope, column)
		if err != nil {
			return nil, err
		}
		output := strings.TrimSpace(aliased.As.String())
		if output == "" {
			output = strings.TrimSpace(column.Name.String())
		}
		if output == "" || hasMySQLCompatibleIdentifier(projection.columns, output) {
			return nil, p.outputLineageError("output column names must be unique")
		}
		projection.columns[output] = origin
		projection.order = append(projection.order, output)
	}
	if len(projection.columns) == 0 {
		return nil, p.outputLineageError("query output projection is empty")
	}
	return projection, nil
}

func (p MySQLCompatibleQueryProvenance) buildLineageScope(
	defaultDatabase string,
	expressions sqlparser.TableExprs,
	sources []plugin.QueryOutputSource,
) (*mysqlCompatibleLineageScope, error) {
	scope := newMySQLCompatibleLineageScope()
	for _, expression := range expressions {
		child, err := p.buildLineageTableExpression(defaultDatabase, expression, sources)
		if err != nil {
			return nil, err
		}
		if err := p.mergeLineageScope(scope, child); err != nil {
			return nil, err
		}
	}
	return scope, nil
}

func (p MySQLCompatibleQueryProvenance) buildLineageTableExpression(
	defaultDatabase string,
	expression sqlparser.TableExpr,
	sources []plugin.QueryOutputSource,
) (*mysqlCompatibleLineageScope, error) {
	switch typed := expression.(type) {
	case *sqlparser.AliasedTableExpr:
		return p.buildAliasedLineageRelation(defaultDatabase, typed, sources)
	case *sqlparser.JoinTableExpr:
		left, err := p.buildLineageTableExpression(defaultDatabase, typed.LeftExpr, sources)
		if err != nil {
			return nil, err
		}
		right, err := p.buildLineageTableExpression(defaultDatabase, typed.RightExpr, sources)
		if err != nil {
			return nil, err
		}
		if err := p.mergeLineageScope(left, right); err != nil {
			return nil, err
		}
		return left, nil
	case *sqlparser.ParenTableExpr:
		return p.buildLineageScope(defaultDatabase, typed.Exprs, sources)
	default:
		return nil, p.outputLineageError("FROM relation is not supported")
	}
}

func (p MySQLCompatibleQueryProvenance) buildAliasedLineageRelation(
	defaultDatabase string,
	expression *sqlparser.AliasedTableExpr,
	sources []plugin.QueryOutputSource,
) (*mysqlCompatibleLineageScope, error) {
	if expression == nil {
		return nil, p.outputLineageError("FROM relation is missing")
	}
	scope := newMySQLCompatibleLineageScope()
	alias := strings.TrimSpace(expression.As.String())
	switch relation := expression.Expr.(type) {
	case sqlparser.TableName:
		database := strings.TrimSpace(relation.Qualifier.String())
		if database == "" {
			database = defaultDatabase
		}
		table := strings.TrimSpace(relation.Name.String())
		index := mysqlCompatibleLineageSourceIndex(sources, database, table)
		if index < 0 {
			return nil, p.outputLineageError("FROM relation is missing from the read set")
		}
		columns, err := p.lineageTableColumns(sources[index], index)
		if err != nil {
			return nil, err
		}
		qualifiers := []string{alias}
		if alias == "" {
			qualifiers = []string{table, database + "." + table}
		}
		if err := p.addLineageRelation(scope, qualifiers, mysqlCompatibleLineageRelation{columns: columns}); err != nil {
			return nil, err
		}
		return scope, nil
	case *sqlparser.Subquery:
		if alias == "" || relation.Select == nil {
			return nil, p.outputLineageError("derived table requires an alias and SELECT")
		}
		projection, err := p.resolveSelectProjection(defaultDatabase, relation.Select, sources)
		if err != nil {
			return nil, err
		}
		if err := p.addLineageRelation(scope, []string{alias}, mysqlCompatibleLineageRelation{columns: projection.columns}); err != nil {
			return nil, err
		}
		return scope, nil
	default:
		return nil, p.outputLineageError("FROM relation is not supported")
	}
}

func newMySQLCompatibleLineageScope() *mysqlCompatibleLineageScope {
	return &mysqlCompatibleLineageScope{
		relations:   make(map[string]mysqlCompatibleLineageRelation),
		unqualified: make(map[string][]mysqlCompatibleLineageOrigin),
	}
}

func (p MySQLCompatibleQueryProvenance) addLineageRelation(scope *mysqlCompatibleLineageScope, qualifiers []string, relation mysqlCompatibleLineageRelation) error {
	if scope == nil || len(relation.columns) == 0 {
		return p.outputLineageError("relation fields are required")
	}
	added := false
	for _, qualifier := range qualifiers {
		qualifier = strings.TrimSpace(qualifier)
		if qualifier == "" {
			continue
		}
		if _, exists := lookupMySQLCompatibleRelation(scope.relations, qualifier); exists {
			return p.outputLineageError("relation qualifier is ambiguous")
		}
		scope.relations[qualifier] = relation
		added = true
	}
	if !added {
		return p.outputLineageError("relation qualifier is required")
	}
	for name, origin := range relation.columns {
		key := strings.ToLower(name)
		scope.unqualified[key] = append(scope.unqualified[key], origin)
	}
	return nil
}

func (p MySQLCompatibleQueryProvenance) mergeLineageScope(target, source *mysqlCompatibleLineageScope) error {
	if target == nil || source == nil {
		return p.outputLineageError("relation scope is incomplete")
	}
	for qualifier, relation := range source.relations {
		if _, exists := lookupMySQLCompatibleRelation(target.relations, qualifier); exists {
			return p.outputLineageError("relation qualifier is ambiguous")
		}
		target.relations[qualifier] = relation
	}
	for name, origins := range source.unqualified {
		target.unqualified[name] = append(target.unqualified[name], origins...)
	}
	return nil
}

func (p MySQLCompatibleQueryProvenance) resolveLineageColumn(scope *mysqlCompatibleLineageScope, column *sqlparser.ColName) (mysqlCompatibleLineageOrigin, error) {
	if scope == nil || column == nil {
		return mysqlCompatibleLineageOrigin{}, p.outputLineageError("column reference is missing")
	}
	name := strings.TrimSpace(column.Name.String())
	if name == "" {
		return mysqlCompatibleLineageOrigin{}, p.outputLineageError("column name is missing")
	}
	qualifier := strings.TrimSpace(column.Qualifier.Name.String())
	if database := strings.TrimSpace(column.Qualifier.Qualifier.String()); database != "" {
		qualifier = database + "." + qualifier
	}
	if qualifier != "" {
		relation, exists := lookupMySQLCompatibleRelation(scope.relations, qualifier)
		if !exists {
			return mysqlCompatibleLineageOrigin{}, p.outputLineageError("column qualifier is unresolved")
		}
		origin, exists := lookupMySQLCompatibleOrigin(relation.columns, name)
		if !exists {
			return mysqlCompatibleLineageOrigin{}, p.outputLineageError("column is absent from the qualified relation")
		}
		return origin, nil
	}
	origins := scope.unqualified[strings.ToLower(name)]
	if len(origins) != 1 {
		return mysqlCompatibleLineageOrigin{}, p.outputLineageError("unqualified column is unresolved or ambiguous")
	}
	return origins[0], nil
}

func (p MySQLCompatibleQueryProvenance) lineageTableColumns(source plugin.QueryOutputSource, sourceIndex int) (map[string]mysqlCompatibleLineageOrigin, error) {
	columns := make(map[string]mysqlCompatibleLineageOrigin, len(source.Fields))
	for _, field := range source.Fields {
		name := strings.TrimSpace(field.Name)
		path := field.Path
		if len(path) == 0 {
			path = []string{name}
		}
		if name == "" || hasMySQLCompatibleIdentifier(columns, name) {
			return nil, p.outputLineageError("source fields must have unique names")
		}
		columns[name] = mysqlCompatibleLineageOrigin{sourceIndex: sourceIndex, sourcePath: append([]string(nil), path...)}
	}
	return columns, nil
}

func mysqlCompatibleLineageSourceIndex(sources []plugin.QueryOutputSource, database, table string) int {
	exact := -1
	folded := -1
	for index, source := range sources {
		segments := plugin.EngineCatalogPathWithoutRoot(source.Path).Segments
		if len(segments) != 2 {
			continue
		}
		sourceDatabase, sourceTable := segments[0].Name, segments[1].Name
		if sourceDatabase == database && sourceTable == table {
			if exact >= 0 {
				return -1
			}
			exact = index
			continue
		}
		if strings.EqualFold(sourceDatabase, database) && strings.EqualFold(sourceTable, table) {
			if folded >= 0 {
				folded = -2
			} else {
				folded = index
			}
		}
	}
	if exact >= 0 {
		return exact
	}
	if folded >= 0 {
		return folded
	}
	return -1
}

func lookupMySQLCompatibleRelation(relations map[string]mysqlCompatibleLineageRelation, name string) (mysqlCompatibleLineageRelation, bool) {
	if relation, exists := relations[name]; exists {
		return relation, true
	}
	var matched mysqlCompatibleLineageRelation
	matches := 0
	for candidate, relation := range relations {
		if strings.EqualFold(candidate, name) {
			matched = relation
			matches++
		}
	}
	return matched, matches == 1
}

func lookupMySQLCompatibleOrigin(columns map[string]mysqlCompatibleLineageOrigin, name string) (mysqlCompatibleLineageOrigin, bool) {
	if origin, exists := columns[name]; exists {
		return origin, true
	}
	var matched mysqlCompatibleLineageOrigin
	matches := 0
	for candidate, origin := range columns {
		if strings.EqualFold(candidate, name) {
			matched = origin
			matches++
		}
	}
	return matched, matches == 1
}

func hasMySQLCompatibleIdentifier[T any](values map[string]T, name string) bool {
	for candidate := range values {
		if strings.EqualFold(candidate, name) {
			return true
		}
	}
	return false
}

func (p MySQLCompatibleQueryProvenance) outputLineageError(reason string) error {
	return fmt.Errorf("%w: %s %s", plugin.ErrQueryOutputLineageUnresolved, p.engineName(), reason)
}
