package postgresql

import (
	"context"
	"database/sql"

	"github.com/addp/common/engine/plugin"
	pgquery "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// The cursor has already bound and locked its relations in this very read
// transaction. Proof queries never run on a fresh pool connection. A failed
// optional proof must not poison the cursor's transaction or change the read.
func (p *PostgreSQLPlugin) observeSessionReadSet(ctx context.Context, tx *sql.Tx, req plugin.QueryRequest) *plugin.QueryReadSet {
	if p.Type() != "postgresql" {
		return nil
	}
	if _, err := tx.ExecContext(ctx, "SAVEPOINT addp_read_evidence"); err != nil {
		return nil
	}
	set, err := p.proveSessionReadSet(ctx, tx, req)
	if err != nil {
		_, _ = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT addp_read_evidence")
	}
	if _, releaseErr := tx.ExecContext(ctx, "RELEASE SAVEPOINT addp_read_evidence"); releaseErr != nil || err != nil {
		return nil
	}
	return set.Clone()
}

func (p *PostgreSQLPlugin) proveSessionReadSet(ctx context.Context, tx *sql.Tx, req plugin.QueryRequest) (*plugin.QueryReadSet, error) {
	dependencies, err := inspectPostgresSQLReadDependencies(req.Query)
	if err != nil || req.EngineID == 0 || !req.Options.ReadOnly || len(dependencies.Relations) == 0 || len(dependencies.Relations) > 200 || len(dependencies.Functions) != 0 {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	parsed, err := pgquery.Parse(req.Query)
	operators := make(map[string]struct{})
	if err != nil || !inspectSessionExpressions(parsed.ProtoReflect(), operators) {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	// Qualified relations and built-in operand types avoid a search-path name
	// shadow becoming a new hidden source. Custom operators/casts/functions and
	// policy/view/inheritance closures remain unsupported in this first slice.
	var firstSchema string
	if err := tx.QueryRowContext(ctx, "SELECT (pg_catalog.current_schemas(true))[1]").Scan(&firstSchema); err != nil || firstSchema != "pg_catalog" {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	for operator := range operators {
		var hidden bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS (
 SELECT 1 FROM pg_catalog.pg_operator o
 JOIN pg_catalog.pg_namespace n ON n.oid=o.oprnamespace
 LEFT JOIN pg_catalog.pg_type l ON l.oid=o.oprleft
 LEFT JOIN pg_catalog.pg_namespace ln ON ln.oid=l.typnamespace
 LEFT JOIN pg_catalog.pg_type r ON r.oid=o.oprright
 LEFT JOIN pg_catalog.pg_namespace rn ON rn.oid=r.typnamespace
 WHERE o.oprname=$1 AND n.nspname<>'pg_catalog' AND pg_catalog.pg_operator_is_visible(o.oid)
 AND (o.oprleft=0 OR ln.nspname='pg_catalog') AND (o.oprright=0 OR rn.nspname='pg_catalog'))`, operator).Scan(&hidden)
		if err != nil || hidden {
			return nil, plugin.ErrQueryReadSetUnresolved
		}
	}
	catalog := &postgresDatabaseReadCatalog{db: tx}
	for _, reference := range dependencies.Relations {
		if reference.Schema == "" {
			return nil, plugin.ErrQueryReadSetUnresolved
		}
		relation, err := catalog.ResolveRelation(ctx, reference)
		if err != nil || relation.Relkind != "r" {
			return nil, plugin.ErrQueryReadSetUnresolved
		}
		var safe bool
		err = tx.QueryRowContext(ctx, `SELECT NOT c.relrowsecurity AND NOT c.relhasrules
 AND EXISTS (SELECT 1 FROM pg_catalog.pg_am am
 JOIN pg_catalog.pg_proc h ON h.oid=am.amhandler JOIN pg_catalog.pg_namespace hn ON hn.oid=h.pronamespace
 WHERE am.oid=c.relam AND hn.nspname='pg_catalog')
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_inherits i WHERE i.inhrelid=c.oid OR i.inhparent=c.oid)
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class idx ON idx.oid=i.indexrelid
 JOIN pg_catalog.pg_am am ON am.oid=idx.relam
 JOIN pg_catalog.pg_proc h ON h.oid=am.amhandler JOIN pg_catalog.pg_namespace hn ON hn.oid=h.pronamespace
 WHERE i.indrelid=c.oid AND (i.indexprs IS NOT NULL OR i.indpred IS NOT NULL OR hn.nspname<>'pg_catalog'
 OR EXISTS (SELECT 1 FROM unnest(i.indclass) opclass JOIN pg_catalog.pg_opclass oc ON oc.oid=opclass
 JOIN pg_catalog.pg_namespace ns ON ns.oid=oc.opcnamespace WHERE ns.nspname<>'pg_catalog')))
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
 JOIN pg_catalog.pg_type t ON t.oid=a.atttypid
 JOIN pg_catalog.pg_namespace n ON n.oid=t.typnamespace
 LEFT JOIN pg_catalog.pg_type element ON element.oid=t.typelem
 LEFT JOIN pg_catalog.pg_namespace en ON en.oid=element.typnamespace
 WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped
 AND (n.nspname<>'pg_catalog' OR (t.typelem<>0 AND en.nspname<>'pg_catalog')))
 FROM pg_catalog.pg_class c WHERE c.oid=$1`, relation.OID).Scan(&safe)
		if err != nil || !safe {
			return nil, plugin.ErrQueryReadSetUnresolved
		}
	}
	return p.resolvePostgresQueryReadSet(ctx, req, dependencies, catalog)
}

// Narrow proof grammar only; execution still consumes the original request.
// The shared PG inspector above owns query dependency/CTE semantics.
func sessionTransparentMessage(message protoreflect.Message) bool {
	return inspectSessionExpressions(message, nil)
}

func inspectSessionExpressions(message protoreflect.Message, operators map[string]struct{}) bool {
	if node, ok := message.Interface().(*pgquery.Node); ok {
		if node.GetTypeCast() != nil || node.GetCollateClause() != nil || node.GetFuncCall() != nil || node.GetRangeTableSample() != nil {
			return false
		}
		if sort := node.GetSortBy(); sort != nil && len(sort.GetUseOp()) != 0 {
			return false
		}
		if expression := node.GetAExpr(); expression != nil {
			if len(expression.GetName()) != 1 {
				return false
			}
			operator := expression.GetName()[0].GetString_().GetSval()
			switch operator {
			case "=", "<>", "!=", "<", ">", "<=", ">=", "+", "-", "*", "/", "%", "||":
			default:
				return false
			}
			if operators != nil {
				operators[operator] = struct{}{}
			}
		}
	}
	ok := true
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.IsList() && field.Kind() == protoreflect.MessageKind {
			list := value.List()
			for i := 0; i < list.Len(); i++ {
				if !inspectSessionExpressions(list.Get(i).Message(), operators) {
					ok = false
					return false
				}
			}
		} else if field.Kind() == protoreflect.MessageKind && !field.IsMap() {
			ok = inspectSessionExpressions(value.Message(), operators)
		}
		return ok
	})
	return ok
}
