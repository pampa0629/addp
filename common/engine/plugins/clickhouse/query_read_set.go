package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	chsql "github.com/AfterShip/clickhouse-sql-parser/parser"
	"github.com/addp/common/engine/plugin"
)

type querySources struct {
	readSet  *plugin.QueryReadSet
	cteNames []string
}

func (p *ClickHousePlugin) resolveQueryReadSet(_ context.Context, info plugin.ConnectionInfo, request plugin.QueryRequest) (*plugin.QueryReadSet, error) {
	sources, err := p.querySources(info, request)
	if err != nil {
		return nil, err
	}
	return sources.readSet, nil
}

func (p *ClickHousePlugin) querySources(info plugin.ConnectionInfo, request plugin.QueryRequest) (*querySources, error) {
	if !request.Options.ReadOnly || request.EngineID == 0 {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	database := plugin.GetString(info, "database")
	statements, err := chsql.NewParser(request.Query).ParseStmts()
	if err != nil || len(statements) != 1 {
		return nil, unsupportedRead("expected one ClickHouse SELECT")
	}
	selectQuery, ok := statements[0].(*chsql.SelectQuery)
	if !ok {
		return nil, unsupportedRead("expected one ClickHouse SELECT")
	}
	var ctes []string
	var tables []*chsql.TableIdentifier
	var placeholderCount int
	if !chsql.Walk(selectQuery, func(node chsql.Expr) bool {
		switch node := node.(type) {
		case *chsql.SelectQuery, *chsql.SubQuery, *chsql.SelectItem, *chsql.WithClause,
			*chsql.JoinExpr, *chsql.JoinTableExpr, *chsql.FromClause, *chsql.OnClause, *chsql.UsingClause,
			*chsql.AliasExpr, *chsql.Ident, *chsql.Path, *chsql.NestedIdentifier, *chsql.ColumnExpr,
			*chsql.NumberLiteral, *chsql.StringLiteral, *chsql.BoolLiteral, *chsql.NullLiteral,
			*chsql.ColumnArgList, *chsql.ColumnExprList, *chsql.ParamExprList,
			*chsql.WhereClause, *chsql.PrewhereClause, *chsql.GroupByClause, *chsql.HavingClause,
			*chsql.OrderByClause, *chsql.OrderExpr, *chsql.LimitClause:
			return true
		case *chsql.TableExpr:
			target := node.Expr
			if alias, ok := target.(*chsql.AliasExpr); ok {
				target = alias.Expr
			}
			switch target.(type) {
			case *chsql.TableIdentifier, *chsql.SubQuery:
				return true
			default:
				return false
			}
		case *chsql.TableIdentifier:
			tables = append(tables, node)
			return true
		case *chsql.CTEStmt:
			alias, isName := node.Expr.(*chsql.Ident)
			_, isSubquery := node.Alias.(*chsql.SelectQuery)
			if !isName || !isSubquery {
				return false
			}
			ctes = append(ctes, alias.Name)
			return true
		case *chsql.PlaceHolder:
			placeholderCount++
			return node.Type == "?"
		case *chsql.BinaryOperation:
			if !strings.EqualFold(string(node.Operation), "in") {
				return true
			}
			// ClickHouse also accepts a table name on the right of IN.
			switch node.RightExpr.(type) {
			case *chsql.SubQuery, *chsql.ParamExprList, *chsql.ColumnExprList:
				return true
			default:
				return false
			}
		case *chsql.FunctionExpr:
			switch node.Name.Name {
			case "count", "sum", "avg", "min", "max", "abs", "round", "lower", "upper",
				"length", "substring", "concat", "coalesce", "if", "ifnull", "nullif",
				"toString", "toInt64", "toUInt64", "toFloat64", "toDate", "toDateTime":
				return true
			}
		}
		return false
	}) {
		return nil, unsupportedRead("query contains a source or expression outside the admitted ClickHouse subset")
	}
	// The native driver treats ? positionally even inside quoted text when
	// arguments are present. Ensure only scalar AST placeholders can be bound.
	if placeholderCount != len(request.Options.Args) ||
		(placeholderCount > 0 && strings.Count(request.Query, "?") != placeholderCount) {
		return nil, unsupportedRead("query parameters are not unambiguous scalar placeholders")
	}
	var paths []plugin.EngineCatalogPath
	var referencedCTEs []string
	for _, table := range tables {
		name := table.Table.Name
		namespace := database
		if table.Database != nil {
			namespace = table.Database.Name
		} else if slices.Contains(ctes, name) {
			if !slices.Contains(referencedCTEs, name) {
				referencedCTEs = append(referencedCTEs, name)
			}
			continue
		}
		if namespace == "" || name == "" || p.isSystemSchema(namespace) {
			return nil, unsupportedRead("query references a non-business catalog")
		}
		paths = append(paths, plugin.TabularItemPath(request.EngineID, plugin.EngineCatalogTermDatabase, namespace, name))
	}
	readSet, err := plugin.NewQueryReadSet(paths...)
	if err != nil {
		return nil, err
	}
	return &querySources{readSet: readSet, cteNames: referencedCTEs}, nil
}

func unsupportedRead(reason string) error {
	return fmt.Errorf("%w: %s", plugin.ErrQueryReadSetUnresolved, reason)
}

func (s *querySources) verify(ctx context.Context, conn *sql.Conn, database string) error {
	var currentDatabase string
	var readOnly uint64
	if err := conn.QueryRowContext(ctx, `SELECT currentDatabase(), getSetting('readonly')`).Scan(&currentDatabase, &readOnly); err != nil {
		return fmt.Errorf("%w: verify ClickHouse session: %w", plugin.ErrQueryReadSetUnresolved, err)
	}
	if currentDatabase != database || readOnly != 1 {
		return unsupportedRead("ClickHouse session database or read-only mode does not match")
	}
	for _, name := range s.cteNames {
		// An unqualified CTE name is safe to omit only when there is no
		// physical or temporary table for native resolution to select instead.
		var exists uint64
		if err := conn.QueryRowContext(ctx, `
			SELECT count() FROM system.tables
			WHERE name = ? AND (database = ? OR is_temporary = 1)
		`, name, database).Scan(&exists); err != nil {
			return fmt.Errorf("%w: verify ClickHouse CTE identity: %w", plugin.ErrQueryReadSetUnresolved, err)
		}
		if exists != 0 {
			return unsupportedRead("a ClickHouse CTE name also resolves to a physical table")
		}
	}
	for _, path := range s.readSet.Paths {
		parts := plugin.EngineCatalogPathWithoutRoot(path).Segments
		var engine, databaseEngine string
		err := conn.QueryRowContext(ctx, `
			SELECT t.engine, d.engine
			FROM system.tables AS t INNER JOIN system.databases AS d ON t.database = d.name
			WHERE t.database = ? AND t.name = ? AND t.is_temporary = 0
			AND (t.database, t.name) NOT IN (
				SELECT database, table FROM system.columns
				WHERE database = ? AND table = ? AND default_kind != ''
			)
		`, parts[0].Name, parts[1].Name, parts[0].Name, parts[1].Name).Scan(&engine, &databaseEngine)
		if errors.Is(err, sql.ErrNoRows) {
			return unsupportedRead("ClickHouse source is unavailable or has unresolved column dependencies")
		}
		if err != nil {
			return fmt.Errorf("%w: verify ClickHouse table: %w", plugin.ErrQueryReadSetUnresolved, err)
		}
		if engine != "MergeTree" || (databaseEngine != "Atomic" && databaseEngine != "Ordinary") {
			return unsupportedRead("ClickHouse source is outside local ordinary MergeTree tables")
		}
	}
	return nil
}
