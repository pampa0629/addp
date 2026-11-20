package postgresql

import (
	"context"

	"github.com/addp/common/engine/plugin"
	pgquery "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var _ plugin.GuardedQueryReadSessionProvider = (*PostgreSQLPlugin)(nil)

func (p *PostgreSQLPlugin) OpenGuardedQueryReadSession(ctx context.Context, prepared plugin.PreparedQuery, authorize plugin.QueryReadAuthorizer) (plugin.GuardedQueryReadSession, error) {
	if authorize == nil || p.Type() != "postgresql" && p.Type() != "gaussdb" {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	session, err := p.openQueryReadSession(ctx, prepared, authorize)
	if err != nil {
		return nil, err
	}
	return session, nil
}

// The existing language inspector owns SELECT/CTE/read-only semantics. This
// native admission slice additionally excludes unproven expressions and binds
// qualified ordinary relations with ONLY and pg_catalog operators. The table
// proof rejects unsupported relation shapes. Indexes are maintained
// by the trusted source administrator.
type guardedPostgresQuery struct {
	sql                string
	readSet            *plugin.QueryReadSet
	dictionarySchema   string
	integerCastColumns []string
}

func prepareGuardedPostgresQuery(ctx context.Context, request plugin.QueryRequest, catalog postgresReadCatalog) (guardedPostgresQuery, error) {
	reject := func() (guardedPostgresQuery, error) { return guardedPostgresQuery{}, plugin.ErrQueryReadSetUnresolved }
	if request.EngineID == 0 || !request.Options.ReadOnly {
		return reject()
	}
	parsed, err := pgquery.Parse(request.Query)
	if err != nil {
		return reject()
	}
	dependencies, err := inspectParsedPostgresReadDependencies(parsed)
	if err != nil || len(dependencies.Relations) > 200 {
		return reject()
	}
	bindGuardedQueryNames(parsed.ProtoReflect())
	bound := make(map[postgresFunctionReference]string)
	plan := guardedPostgresQuery{}
	for _, ref := range dependencies.Functions {
		if ref.Schema == "pg_catalog" && sessionOutputFunction(ref.Name) || bound[ref] != "" {
			continue
		}
		if catalog == nil {
			return reject()
		}
		candidates, err := catalog.FunctionCandidates(ctx, ref)
		if err != nil {
			return guardedPostgresQuery{}, postgresQueryReadSetCatalogError("resolve cursor function", err)
		}
		if len(candidates) == 0 {
			return reject()
		}
		schema := candidates[0].Schema
		for _, candidate := range candidates {
			operation := postgresExtensionReadOperation(candidate)
			nativeCount := ref.CountStar && schema == "pg_catalog" && candidate.Kind == "a"
			if candidate.Schema != schema || !isTransparentPostgresReadFunction(candidate) || operation == "" && !nativeCount {
				return guardedPostgresQuery{}, unresolvedPostgresFunction(ctx, request.EngineID, candidate, "unverified_cursor_operation")
			}
			if operation == "dictionary" {
				plan.dictionarySchema = schema
			}
		}
		bound[ref] = schema
	}
	paths := make([]plugin.EngineCatalogPath, 0, len(dependencies.Relations))
	for i, relation := range dependencies.Relations {
		if relation.Schema == "" {
			if catalog == nil {
				return reject()
			}
			resolved, err := catalog.ResolveRelation(ctx, relation)
			if err != nil {
				return guardedPostgresQuery{}, postgresQueryReadSetCatalogError("resolve cursor relation", err)
			}
			if resolved.Relkind != "r" || resolved.Schema == "" || resolved.Name != relation.Name {
				return reject()
			}
			relation.Schema = resolved.Schema
		}
		node := dependencies.sourceNodes[i]
		node.Schemaname = relation.Schema
		node.Inh = false
		paths = append(paths, plugin.TabularItemPath(request.EngineID, "schema", relation.Schema, relation.Name))
	}
	// Scope of direct field conversion is a single physical FROM source.
	// Nested queries and derived fields retain the existing expression rules.
	allowedCasts := make(map[*pgquery.TypeCast]bool)
	selectStmt := parsed.Stmts[0].Stmt.GetSelectStmt()
	if len(dependencies.sourceNodes) == 1 && len(selectStmt.FromClause) == 1 && selectStmt.FromClause[0].GetRangeVar() == dependencies.sourceNodes[0] {
		for _, target := range selectStmt.TargetList {
			cast := target.GetResTarget().GetVal().GetTypeCast()
			if cast == nil || !nativePostgresIntegerType(cast.GetTypeName()) {
				continue
			}
			fields := cast.GetArg().GetColumnRef().GetFields()
			if len(fields) != 1 || fields[0].GetString_() == nil {
				continue
			}
			allowedCasts[cast] = true
			plan.integerCastColumns = append(plan.integerCastColumns, fields[0].GetString_().GetSval())
		}
	}
	if !inspectSessionExpressions(parsed.ProtoReflect(), nil, bound, allowedCasts) {
		return reject()
	}
	plan.readSet, err = plugin.NewQueryReadSet(paths...)
	if err != nil {
		return guardedPostgresQuery{}, err
	}
	plan.sql, err = pgquery.Deparse(parsed)
	if err != nil {
		return reject()
	}
	return plan, nil
}

func nativePostgresIntegerType(name *pgquery.TypeName) bool {
	parts := name.GetNames()
	if len(parts) != 2 || parts[0].GetString_().GetSval() != "pg_catalog" ||
		len(name.GetArrayBounds()) != 0 || len(name.GetTypmods()) != 0 || name.GetSetof() || name.GetPctType() {
		return false
	}
	switch parts[1].GetString_().GetSval() {
	case "int2", "int4", "int8":
		return true
	default:
		return false
	}
}

func bindGuardedQueryNames(message protoreflect.Message) {
	if node, ok := message.Interface().(*pgquery.Node); ok {
		if cast := node.GetTypeCast(); cast != nil && cast.GetArg().GetParamRef() != nil {
			name := cast.GetTypeName()
			if len(name.GetNames()) == 1 && name.Names[0].GetString_().GetSval() == "text" {
				name.Names = append([]*pgquery.Node{pgquery.MakeStrNode("pg_catalog")}, name.Names...)
			}
		}
		if expression := node.GetAExpr(); expression != nil && len(expression.Name) == 1 {
			expression.Name = append([]*pgquery.Node{{Node: &pgquery.Node_String_{String_: &pgquery.String{Sval: "pg_catalog"}}}}, expression.Name...)
		}
	}
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.IsList() && field.Kind() == protoreflect.MessageKind {
			list := value.List()
			for i := 0; i < list.Len(); i++ {
				bindGuardedQueryNames(list.Get(i).Message())
			}
		} else if field.Kind() == protoreflect.MessageKind && !field.IsMap() {
			bindGuardedQueryNames(value.Message())
		}
		return true
	})
}
