package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/lib/pq"
	pgquery "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type postgresRelationReference struct {
	Schema string
	Name   string
}

type postgresResolvedRelation struct {
	OID     int64
	Schema  string
	Name    string
	Relkind string
}

type postgresFunctionReference struct {
	Schema        string
	Name          string
	ArgumentCount int
}

type postgresResolvedFunction struct {
	OID             int64
	Schema          string
	Name            string
	Language        string
	Kind            string
	ReturnsSet      bool
	Volatility      string
	SecurityDefiner bool
	Extension       string
}

type postgresQueryReadDependencies struct {
	Relations []postgresRelationReference
	Functions []postgresFunctionReference
}

type postgresReadCatalog interface {
	ResolveRelation(context.Context, postgresRelationReference) (postgresResolvedRelation, error)
	FunctionCandidates(context.Context, postgresFunctionReference) ([]postgresResolvedFunction, error)
	ViewDependencies(context.Context, int64) ([]postgresResolvedRelation, error)
	ViewFunctionDependencies(context.Context, int64) ([]postgresResolvedFunction, error)
}

type postgresSQLReadInspector struct {
	references []postgresRelationReference
	functions  []postgresFunctionReference
}

func (p *PostgreSQLPlugin) resolvePreparedQueryReadSet(
	ctx context.Context,
	connInfo plugin.ConnectionInfo,
	req plugin.QueryRequest,
) (*plugin.QueryReadSet, error) {
	if !req.Options.ReadOnly || req.EngineID == 0 || !strings.EqualFold(strings.TrimSpace(req.Language), "sql") {
		return nil, fmt.Errorf("%w: PostgreSQL query must be a read-only SQL query with an engine", plugin.ErrQueryReadSetUnresolved)
	}
	dependencies, err := inspectPostgresSQLReadDependencies(req.Query)
	if err != nil {
		return nil, err
	}

	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("%w: build PostgreSQL connection: %v", plugin.ErrQueryReadSetUnresolved, err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: open PostgreSQL catalog connection: %v", plugin.ErrQueryReadSetUnresolved, err)
	}
	defer db.Close() //nolint:errcheck

	return p.resolvePostgresQueryReadSet(ctx, req, dependencies, &postgresDatabaseReadCatalog{db: db})
}

func inspectPostgresSQLReadDependencies(query string) (postgresQueryReadDependencies, error) {
	parsed, err := pgquery.Parse(strings.TrimSpace(query))
	if err != nil || len(parsed.GetStmts()) != 1 || parsed.GetStmts()[0].GetStmt().GetSelectStmt() == nil {
		return postgresQueryReadDependencies{}, fmt.Errorf("%w: PostgreSQL query must contain exactly one SELECT", plugin.ErrQueryReadSetUnresolved)
	}
	inspector := &postgresSQLReadInspector{}
	if err := inspector.inspectNode(parsed.Stmts[0].Stmt, nil); err != nil {
		return postgresQueryReadDependencies{}, err
	}
	return postgresQueryReadDependencies{Relations: inspector.references, Functions: inspector.functions}, nil
}

func (i *postgresSQLReadInspector) inspectNode(node *pgquery.Node, scope map[string]struct{}) error {
	if node == nil {
		return nil
	}
	if selectStmt := node.GetSelectStmt(); selectStmt != nil {
		return i.inspectSelect(selectStmt, scope)
	}
	if relation := node.GetRangeVar(); relation != nil {
		return i.inspectRangeVar(relation, scope)
	}
	if node.GetInsertStmt() != nil || node.GetUpdateStmt() != nil || node.GetDeleteStmt() != nil || node.GetMergeStmt() != nil {
		return postgresQueryReadSetError("data modification substatement is not supported")
	}
	if node.GetRangeFunction() != nil || node.GetRangeTableFunc() != nil || node.GetJsonTable() != nil {
		return postgresQueryReadSetError("table function source is not supported")
	}
	if function := node.GetFuncCall(); function != nil {
		return i.inspectFunction(function, scope)
	}
	return i.inspectMessage(node.ProtoReflect(), scope, "")
}

func (i *postgresSQLReadInspector) inspectFunction(function *pgquery.FuncCall, scope map[string]struct{}) error {
	parts := make([]string, 0, len(function.GetFuncname()))
	for _, rawPart := range function.GetFuncname() {
		part := strings.TrimSpace(rawPart.GetString_().GetSval())
		if part == "" {
			return postgresQueryReadSetError("function name is unresolved")
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 || len(parts) > 2 {
		return postgresQueryReadSetError("cross-database or unresolved function name is not supported")
	}
	for _, argument := range function.GetArgs() {
		if argument.GetNamedArgExpr() != nil {
			return postgresQueryReadSetError("named function arguments are not supported")
		}
	}
	reference := postgresFunctionReference{Name: parts[len(parts)-1], ArgumentCount: len(function.GetArgs())}
	if len(parts) == 2 {
		reference.Schema = parts[0]
	}
	i.functions = append(i.functions, reference)
	return i.inspectMessage(function.ProtoReflect(), scope, "funcname")
}

func (i *postgresSQLReadInspector) inspectSelect(stmt *pgquery.SelectStmt, inherited map[string]struct{}) error {
	if stmt.GetIntoClause() != nil || len(stmt.GetLockingClause()) != 0 {
		return postgresQueryReadSetError("SELECT INTO and row locking are not read-only query sources")
	}
	scope := clonePostgresReadScope(inherited)
	withClause := stmt.GetWithClause()
	if withClause != nil && withClause.GetRecursive() {
		for _, rawCTE := range withClause.GetCtes() {
			cte := rawCTE.GetCommonTableExpr()
			if cte == nil || strings.TrimSpace(cte.GetCtename()) == "" {
				return postgresQueryReadSetError("invalid recursive CTE")
			}
			scope[cte.GetCtename()] = struct{}{}
		}
	}
	if withClause != nil {
		for _, rawCTE := range withClause.GetCtes() {
			cte := rawCTE.GetCommonTableExpr()
			if cte == nil || cte.GetCtequery() == nil || strings.TrimSpace(cte.GetCtename()) == "" {
				return postgresQueryReadSetError("invalid CTE")
			}
			if err := i.inspectNode(cte.GetCtequery(), scope); err != nil {
				return err
			}
			if !withClause.GetRecursive() {
				scope[cte.GetCtename()] = struct{}{}
			}
		}
	}
	return i.inspectMessage(stmt.ProtoReflect(), scope, "with_clause")
}

func (i *postgresSQLReadInspector) inspectRangeVar(relation *pgquery.RangeVar, scope map[string]struct{}) error {
	if strings.TrimSpace(relation.GetCatalogname()) != "" {
		return postgresQueryReadSetError("cross-database relation is not supported")
	}
	name := strings.TrimSpace(relation.GetRelname())
	if name == "" {
		return postgresQueryReadSetError("relation name is empty")
	}
	if relation.GetSchemaname() == "" {
		if _, isCTE := scope[name]; isCTE {
			return nil
		}
	}
	i.references = append(i.references, postgresRelationReference{
		Schema: strings.TrimSpace(relation.GetSchemaname()),
		Name:   name,
	})
	return nil
}

func (i *postgresSQLReadInspector) inspectMessage(message protoreflect.Message, scope map[string]struct{}, skipField string) error {
	if !message.IsValid() {
		return nil
	}
	fields := message.Descriptor().Fields()
	for index := 0; index < fields.Len(); index++ {
		field := fields.Get(index)
		if string(field.Name()) == skipField || !message.Has(field) || field.Kind() != protoreflect.MessageKind {
			continue
		}
		value := message.Get(field)
		if field.IsList() {
			list := value.List()
			for itemIndex := 0; itemIndex < list.Len(); itemIndex++ {
				if err := i.inspectReflectedMessage(list.Get(itemIndex).Message(), scope); err != nil {
					return err
				}
			}
			continue
		}
		if err := i.inspectReflectedMessage(value.Message(), scope); err != nil {
			return err
		}
	}
	return nil
}

func (i *postgresSQLReadInspector) inspectReflectedMessage(message protoreflect.Message, scope map[string]struct{}) error {
	switch typed := message.Interface().(type) {
	case *pgquery.Node:
		return i.inspectNode(typed, scope)
	case *pgquery.SelectStmt:
		return i.inspectSelect(typed, scope)
	default:
		return i.inspectMessage(message, scope, "")
	}
}

func (p *PostgreSQLPlugin) resolvePostgresQueryReadSet(
	ctx context.Context,
	req plugin.QueryRequest,
	dependencies postgresQueryReadDependencies,
	catalog postgresReadCatalog,
) (*plugin.QueryReadSet, error) {
	if err := validatePostgresFunctionReferences(ctx, dependencies.Functions, catalog); err != nil {
		return nil, err
	}
	paths := make([]plugin.EngineCatalogPath, 0, len(dependencies.Relations))
	seen := make(map[int64]struct{}, len(dependencies.Relations))
	for _, reference := range dependencies.Relations {
		relation, err := catalog.ResolveRelation(ctx, reference)
		if err != nil {
			return nil, postgresQueryReadSetCatalogError("resolve relation", err)
		}
		resolved, err := p.expandPostgresReadRelation(ctx, req.EngineID, relation, catalog, seen)
		if err != nil {
			return nil, err
		}
		paths = append(paths, resolved...)
	}
	readSet, err := plugin.NewQueryReadSet(paths...)
	if err != nil {
		return nil, err
	}
	if err := plugin.ValidateQueryReadSet(req, readSet); err != nil {
		return nil, err
	}
	return readSet, nil
}

func (p *PostgreSQLPlugin) expandPostgresReadRelation(
	ctx context.Context,
	engineID uint,
	relation postgresResolvedRelation,
	catalog postgresReadCatalog,
	seen map[int64]struct{},
) ([]plugin.EngineCatalogPath, error) {
	if relation.OID == 0 || strings.TrimSpace(relation.Schema) == "" || strings.TrimSpace(relation.Name) == "" {
		return nil, postgresQueryReadSetError("resolved relation is incomplete")
	}
	if _, exists := seen[relation.OID]; exists {
		return nil, nil
	}
	seen[relation.OID] = struct{}{}
	if p.isSystemSchema(relation.Schema) {
		return nil, postgresQueryReadSetError("system catalog relation is not an Engine Catalog leaf")
	}

	kind, err := postgresReadRelationKind(relation.Relkind)
	if err != nil {
		return nil, err
	}
	paths := []plugin.EngineCatalogPath{plugin.EngineCatalogBranchLeafPath(
		p.EngineCatalogModel(), engineID,
		plugin.EngineCatalogTermSchema, relation.Schema,
		plugin.EngineCatalogTermTable, kind, relation.Name,
	)}
	if relation.Relkind != "v" {
		return paths, nil
	}

	functionDependencies, err := catalog.ViewFunctionDependencies(ctx, relation.OID)
	if err != nil {
		return nil, postgresQueryReadSetCatalogError("read view function dependencies", err)
	}
	for _, function := range functionDependencies {
		if !isTransparentPostgresReadFunction(function) {
			return nil, fmt.Errorf(
				"%w: PostgreSQL view %s.%s depends on unresolved function %s.%s",
				plugin.ErrQueryReadSetUnresolved, relation.Schema, relation.Name, function.Schema, function.Name,
			)
		}
	}
	dependencies, err := catalog.ViewDependencies(ctx, relation.OID)
	if err != nil {
		return nil, postgresQueryReadSetCatalogError("read view dependencies", err)
	}
	for _, dependency := range dependencies {
		expanded, err := p.expandPostgresReadRelation(ctx, engineID, dependency, catalog, seen)
		if err != nil {
			return nil, err
		}
		paths = append(paths, expanded...)
	}
	return paths, nil
}

func validatePostgresFunctionReferences(
	ctx context.Context,
	references []postgresFunctionReference,
	catalog postgresReadCatalog,
) error {
	for _, reference := range references {
		candidates, err := catalog.FunctionCandidates(ctx, reference)
		if err != nil {
			return postgresQueryReadSetCatalogError("resolve function", err)
		}
		if len(candidates) == 0 {
			return postgresQueryReadSetError("function dependency cannot be resolved")
		}
		for _, candidate := range candidates {
			if !isTransparentPostgresReadFunction(candidate) {
				return postgresQueryReadSetError("function dependency cannot be proven")
			}
		}
	}
	return nil
}

func isTransparentPostgresReadFunction(function postgresResolvedFunction) bool {
	if function.OID == 0 || function.ReturnsSet || function.SecurityDefiner {
		return false
	}
	switch function.Kind {
	case "f", "a", "w":
	default:
		return false
	}
	if function.Volatility != "i" && function.Volatility != "s" {
		return false
	}
	trustedBuiltin := function.Schema == "pg_catalog" && function.Language == "internal"
	trustedExtension := strings.EqualFold(strings.TrimSpace(function.Extension), "postgis")
	return trustedBuiltin || trustedExtension
}

func postgresReadRelationKind(relkind string) (string, error) {
	switch strings.TrimSpace(relkind) {
	case "r", "p":
		return plugin.EngineCatalogKindTable, nil
	case "v":
		return "view", nil
	case "m":
		return "materialized_view", nil
	case "f":
		return "", postgresQueryReadSetError("foreign table dependency is not an Engine Catalog leaf closure")
	default:
		return "", postgresQueryReadSetError("unsupported PostgreSQL relation kind " + relkind)
	}
}

type postgresDatabaseReadCatalog struct {
	db *sql.DB
}

func (c *postgresDatabaseReadCatalog) ResolveRelation(ctx context.Context, reference postgresRelationReference) (postgresResolvedRelation, error) {
	identifier := pq.QuoteIdentifier(reference.Name)
	if reference.Schema != "" {
		identifier = pq.QuoteIdentifier(reference.Schema) + "." + identifier
	}
	var relation postgresResolvedRelation
	err := c.db.QueryRowContext(ctx, `
		SELECT cls.oid::bigint, ns.nspname, cls.relname, cls.relkind::text
		FROM pg_catalog.pg_class cls
		JOIN pg_catalog.pg_namespace ns ON ns.oid = cls.relnamespace
		WHERE cls.oid = pg_catalog.to_regclass($1)
	`, identifier).Scan(&relation.OID, &relation.Schema, &relation.Name, &relation.Relkind)
	if err != nil {
		return postgresResolvedRelation{}, err
	}
	return relation, nil
}

func (c *postgresDatabaseReadCatalog) FunctionCandidates(ctx context.Context, reference postgresFunctionReference) ([]postgresResolvedFunction, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT proc.oid::bigint, namespace.nspname, proc.proname, language.lanname,
		       proc.prokind::text, proc.proretset, proc.provolatile::text, proc.prosecdef,
		       COALESCE(extension.extname, '')
		FROM pg_catalog.pg_proc proc
		JOIN pg_catalog.pg_namespace namespace ON namespace.oid = proc.pronamespace
		JOIN pg_catalog.pg_language language ON language.oid = proc.prolang
		LEFT JOIN pg_catalog.pg_depend extension_dependency
		  ON extension_dependency.classid = 'pg_catalog.pg_proc'::pg_catalog.regclass
		 AND extension_dependency.objid = proc.oid
		 AND extension_dependency.deptype = 'e'
		LEFT JOIN pg_catalog.pg_extension extension ON extension.oid = extension_dependency.refobjid
		WHERE proc.proname = $1
		  AND (($2 <> '' AND namespace.nspname = $2) OR ($2 = '' AND pg_catalog.pg_function_is_visible(proc.oid)))
		  AND $3::integer >= proc.pronargs::integer - proc.pronargdefaults::integer
		      - CASE WHEN proc.provariadic <> 0 THEN 1 ELSE 0 END
		  AND (proc.provariadic <> 0 OR $3::integer <= proc.pronargs::integer)
		ORDER BY namespace.nspname, proc.oid
	`, reference.Name, reference.Schema, reference.ArgumentCount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	functions := make([]postgresResolvedFunction, 0)
	for rows.Next() {
		var function postgresResolvedFunction
		if err := rows.Scan(
			&function.OID, &function.Schema, &function.Name, &function.Language,
			&function.Kind, &function.ReturnsSet, &function.Volatility, &function.SecurityDefiner, &function.Extension,
		); err != nil {
			return nil, err
		}
		functions = append(functions, function)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return functions, nil
}

func (c *postgresDatabaseReadCatalog) ViewDependencies(ctx context.Context, oid int64) ([]postgresResolvedRelation, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT DISTINCT dependency.oid::bigint, namespace.nspname, dependency.relname, dependency.relkind::text
		FROM pg_catalog.pg_rewrite rewrite
		JOIN pg_catalog.pg_depend depend
		  ON depend.classid = 'pg_catalog.pg_rewrite'::pg_catalog.regclass
		 AND depend.objid = rewrite.oid
		 AND depend.refclassid = 'pg_catalog.pg_class'::pg_catalog.regclass
		JOIN pg_catalog.pg_class dependency ON dependency.oid = depend.refobjid
		JOIN pg_catalog.pg_namespace namespace ON namespace.oid = dependency.relnamespace
		WHERE rewrite.ev_class = $1::oid
		  AND dependency.oid <> rewrite.ev_class
		  AND depend.deptype = 'n'
		ORDER BY namespace.nspname, dependency.relname
	`, oid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var relations []postgresResolvedRelation
	for rows.Next() {
		var relation postgresResolvedRelation
		if err := rows.Scan(&relation.OID, &relation.Schema, &relation.Name, &relation.Relkind); err != nil {
			return nil, err
		}
		relations = append(relations, relation)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return relations, nil
}

func (c *postgresDatabaseReadCatalog) ViewFunctionDependencies(ctx context.Context, oid int64) ([]postgresResolvedFunction, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT DISTINCT function.oid::bigint, namespace.nspname, function.proname, language.lanname,
		       function.prokind::text, function.proretset, function.provolatile::text, function.prosecdef,
		       COALESCE(extension.extname, '')
		FROM pg_catalog.pg_rewrite rewrite
		JOIN pg_catalog.pg_depend depend
		  ON depend.classid = 'pg_catalog.pg_rewrite'::pg_catalog.regclass
		 AND depend.objid = rewrite.oid
		 AND depend.refclassid = 'pg_catalog.pg_proc'::pg_catalog.regclass
		JOIN pg_catalog.pg_proc function ON function.oid = depend.refobjid
		JOIN pg_catalog.pg_namespace namespace ON namespace.oid = function.pronamespace
		JOIN pg_catalog.pg_language language ON language.oid = function.prolang
		LEFT JOIN pg_catalog.pg_depend extension_dependency
		  ON extension_dependency.classid = 'pg_catalog.pg_proc'::pg_catalog.regclass
		 AND extension_dependency.objid = function.oid
		 AND extension_dependency.deptype = 'e'
		LEFT JOIN pg_catalog.pg_extension extension ON extension.oid = extension_dependency.refobjid
		WHERE rewrite.ev_class = $1::oid
		  AND depend.deptype = 'n'
		ORDER BY 2, 3, 1
	`, oid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	functions := make([]postgresResolvedFunction, 0)
	for rows.Next() {
		var function postgresResolvedFunction
		if err := rows.Scan(
			&function.OID, &function.Schema, &function.Name, &function.Language,
			&function.Kind, &function.ReturnsSet, &function.Volatility, &function.SecurityDefiner, &function.Extension,
		); err != nil {
			return nil, err
		}
		functions = append(functions, function)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return functions, nil
}

func clonePostgresReadScope(source map[string]struct{}) map[string]struct{} {
	cloned := make(map[string]struct{}, len(source)+4)
	for name := range source {
		cloned[name] = struct{}{}
	}
	return cloned
}

func postgresQueryReadSetError(message string) error {
	return fmt.Errorf("%w: %s", plugin.ErrQueryReadSetUnresolved, message)
}

func postgresQueryReadSetCatalogError(operation string, err error) error {
	return fmt.Errorf("%w: %s: %v", plugin.ErrQueryReadSetUnresolved, operation, err)
}
