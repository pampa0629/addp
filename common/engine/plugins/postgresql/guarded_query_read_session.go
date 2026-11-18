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
// proof rejects unsupported relation shapes and types. Indexes are maintained
// by the trusted source administrator.
func prepareGuardedPostgresQuery(request plugin.QueryRequest) (string, *plugin.QueryReadSet, error) {
	dependencies, err := inspectPostgresSQLReadDependencies(request.Query)
	if err != nil || request.EngineID == 0 || !request.Options.ReadOnly || len(dependencies.Relations) > 200 {
		return "", nil, plugin.ErrQueryReadSetUnresolved
	}
	parsed, err := pgquery.Parse(request.Query)
	if err != nil {
		return "", nil, plugin.ErrQueryReadSetUnresolved
	}
	bindGuardedQueryNames(parsed.ProtoReflect())
	if !inspectSessionExpressions(parsed.ProtoReflect(), nil) {
		return "", nil, plugin.ErrQueryReadSetUnresolved
	}
	paths := make([]plugin.EngineCatalogPath, 0, len(dependencies.Relations))
	for _, relation := range dependencies.Relations {
		if relation.Schema == "" {
			return "", nil, plugin.ErrQueryReadSetUnresolved
		}
		paths = append(paths, plugin.TabularItemPath(request.EngineID, "schema", relation.Schema, relation.Name))
	}
	set, err := plugin.NewQueryReadSet(paths...)
	if err != nil {
		return "", nil, err
	}
	query, err := pgquery.Deparse(parsed)
	if err != nil {
		return "", nil, plugin.ErrQueryReadSetUnresolved
	}
	return query, set, nil
}

func bindGuardedQueryNames(message protoreflect.Message) {
	if node, ok := message.Interface().(*pgquery.Node); ok {
		if cast := node.GetTypeCast(); cast != nil && cast.GetArg().GetParamRef() != nil {
			name := cast.GetTypeName()
			if len(name.GetNames()) == 1 && name.Names[0].GetString_().GetSval() == "text" {
				name.Names = append([]*pgquery.Node{pgquery.MakeStrNode("pg_catalog")}, name.Names...)
			}
		}
		if relation := node.GetRangeVar(); relation != nil && relation.GetSchemaname() != "" {
			relation.Inh = false
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
