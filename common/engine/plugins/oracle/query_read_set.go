package oracle

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/addp/common/engine/plugin"
	pg "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type oracleSQLRead struct {
	sql         string
	identifiers map[int]int
	tokens      []*pg.ScanToken
	schema      string
	engineID    uint
	paths       []plugin.EngineCatalogPath
}

// The shared parser covers the common SELECT grammar. Oracle owns actual
// resolution through EXPAND_SQL_TEXT on the execution transaction.
func inspectOracleReadSQL(query, schema string, engineID uint, expanded bool) (*plugin.QueryReadSet, error) {
	scan, err := pg.Scan(query)
	if err != nil {
		return nil, oracleReadUnresolved("cannot scan Oracle SELECT")
	}
	read := oracleSQLRead{sql: query, tokens: scan.Tokens, identifiers: make(map[int]int), schema: schema, engineID: engineID}
	analysisSQL := []byte(query)
	for i, token := range scan.Tokens {
		start, end := int(token.Start), int(token.End)
		read.identifiers[start] = i
		if query[start:end] != ":" || i+1 >= len(scan.Tokens) || scan.Tokens[i+1].Start != token.End {
			continue
		}
		next := scan.Tokens[i+1]
		number := query[next.Start:next.End]
		if expanded && strings.HasPrefix(number, "B") {
			number = number[1:]
		}
		if number == "" || strings.Trim(number, "0123456789") != "" {
			continue
		}
		for j := start; j < int(next.End); j++ {
			analysisSQL[j] = ' '
		}
		copy(analysisSQL[start:], "$"+number)
	}
	tree, err := pg.Parse(string(analysisSQL))
	if err != nil || len(tree.GetStmts()) != 1 || tree.Stmts[0].Stmt.GetSelectStmt() == nil {
		return nil, oracleReadUnresolved("expected one supported Oracle SELECT")
	}
	if err := read.walk(tree.Stmts[0].Stmt.ProtoReflect(), nil); err != nil {
		return nil, err
	}
	return plugin.NewQueryReadSet(read.paths...)
}

func oracleReadUnresolved(reason string) error {
	return fmt.Errorf("%w: %s", plugin.ErrQueryReadSetUnresolved, reason)
}

func (r *oracleSQLRead) walk(msg protoreflect.Message, scope map[string]bool) error {
	var skip string
	switch node := msg.Interface().(type) {
	case *pg.Node, *pg.JoinExpr, *pg.RangeSubselect, *pg.Alias, *pg.ResTarget,
		*pg.SortBy, *pg.SubLink, *pg.A_Expr, *pg.A_Const, *pg.A_Star, *pg.BoolExpr,
		*pg.NullTest, *pg.BooleanTest, *pg.ParamRef, *pg.CaseExpr, *pg.CaseWhen,
		*pg.CoalesceExpr, *pg.Integer, *pg.Float, *pg.Boolean, *pg.String, *pg.List:
	case *pg.ColumnRef:
		if len(node.Fields) > 1 {
			switch strings.ToUpper(node.Fields[len(node.Fields)-1].GetString_().GetSval()) {
			case "NEXTVAL", "CURRVAL":
				return oracleReadUnresolved("Oracle sequence pseudocolumn is outside the table read boundary")
			}
		}
	case *pg.SelectStmt:
		if node.IntoClause != nil || len(node.LockingClause) > 0 || len(node.ValuesLists) > 0 || len(node.WindowClause) > 0 {
			return oracleReadUnresolved("SELECT clause has no Oracle source proof")
		}
		if node.WithClause != nil {
			if node.WithClause.Recursive {
				return oracleReadUnresolved("recursive CTE is outside Oracle admission")
			}
			if scope == nil {
				scope = make(map[string]bool)
			} else {
				scope = maps.Clone(scope)
			}
			for _, entry := range node.WithClause.Ctes {
				cte := entry.GetCommonTableExpr()
				if cte == nil || cte.Ctequery.GetSelectStmt() == nil {
					return oracleReadUnresolved("unsupported Oracle CTE")
				}
				if err := r.walk(cte.Ctequery.ProtoReflect(), scope); err != nil {
					return err
				}
				names := r.namesAt(cte.Location, 1)
				if len(names) != 1 {
					return oracleReadUnresolved("cannot resolve Oracle CTE name")
				}
				scope[names[0]] = true
			}
			skip = "with_clause"
		}
	case *pg.RangeVar:
		if node.Catalogname != "" {
			return oracleReadUnresolved("cross-database Oracle source")
		}
		count := 1
		if node.Schemaname != "" {
			count = 2
		}
		names := r.namesAt(node.Location, count)
		if len(names) != count {
			return oracleReadUnresolved("cannot resolve Oracle relation name")
		}
		schema, name := r.schema, names[count-1]
		if count == 2 {
			schema = names[0]
		} else if scope[name] {
			return nil
		}
		if schema == "SYS" && name == "DUAL" {
			return nil
		}
		if schema == "" || name == "" || (&OraclePlugin{}).isSystemSchema(schema) {
			return oracleReadUnresolved("Oracle business schema cannot be established")
		}
		r.paths = append(r.paths, plugin.TabularItemPath(r.engineID, plugin.EngineCatalogTermSchema, schema, name))
		return nil
	case *pg.FuncCall:
		names := r.namesAt(node.Location, len(node.Funcname))
		if !isOracleReadFunction(names) || node.Over != nil {
			return oracleReadUnresolved("Oracle function read dependencies are unproven")
		}
	default:
		return oracleReadUnresolved("query includes an unsupported Oracle SQL expression")
	}
	var err error
	msg.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.Kind() != protoreflect.MessageKind || string(field.Name()) == skip {
			return true
		}
		if field.IsList() {
			list := value.List()
			for i := 0; i < list.Len() && err == nil; i++ {
				err = r.walk(list.Get(i).Message(), scope)
			}
		} else {
			err = r.walk(value.Message(), scope)
		}
		return err == nil
	})
	return err
}

func (r *oracleSQLRead) namesAt(location int32, count int) []string {
	index, ok := r.identifiers[int(location)]
	if !ok || count == 0 || index+2*(count-1) >= len(r.tokens) {
		return nil
	}
	names := make([]string, 0, count)
	for i := 0; i < count; i++ {
		token := r.tokens[index+2*i]
		name := r.sql[token.Start:token.End]
		if strings.HasPrefix(name, `"`) && strings.HasSuffix(name, `"`) {
			name = strings.ReplaceAll(name[1:len(name)-1], `""`, `"`)
		} else {
			name = strings.ToUpper(name)
		}
		names = append(names, name)
		if i+1 < count {
			dot := r.tokens[index+2*i+1]
			if r.sql[dot.Start:dot.End] != "." {
				return nil
			}
		}
	}
	return names
}

func isOracleReadFunction(names []string) bool {
	if len(names) == 1 {
		switch names[0] {
		case "COUNT", "SUM", "AVG", "MIN", "MAX", "ABS", "ROUND", "LOWER", "UPPER",
			"LENGTH", "SUBSTR", "CONCAT", "NVL", "NULLIF", "ROW_NUMBER", "TO_CHAR",
			"TO_NUMBER", "TO_DATE", "TO_TIMESTAMP":
			return true
		}
	}
	if len(names) != 3 || names[0] != "MDSYS" {
		return false
	}
	switch names[1] {
	case "SDO_CS":
		return names[2] == "TRANSFORM"
	case "SDO_UTIL":
		return names[2] == "TO_GEOJSON" || names[2] == "TO_WKTGEOMETRY" || names[2] == "TO_WKBGEOMETRY"
	}
	return false
}

func (p *OraclePlugin) resolveQueryReadSet(_ context.Context, info plugin.ConnectionInfo, request plugin.QueryRequest) (*plugin.QueryReadSet, error) {
	if !request.Options.ReadOnly || request.EngineID == 0 {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	return inspectOracleReadSQL(request.Query, strings.ToUpper(plugin.GetString(info, "user")), request.EngineID, false)
}
