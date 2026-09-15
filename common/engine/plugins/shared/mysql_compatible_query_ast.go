package shared

import (
	"fmt"
	"strings"

	"github.com/dolthub/vitess/go/vt/sqlparser"
)

// parseMySQLReadQuery is the single AST entry for read-set and output-lineage
// analysis. Nonrecursive CTEs become scoped derived relations in the analysis
// tree only; execution retains the original parameter-bound SQL.
func parseMySQLReadQuery(query string) (sqlparser.Statement, error) {
	if len(query) > 1<<20 {
		return nil, fmt.Errorf("query exceeds analysis size budget")
	}
	if strings.Contains(query, "/*!") {
		return nil, fmt.Errorf("executable comments are unsupported")
	}
	statement, err := sqlparser.Parse(strings.TrimSpace(query))
	if err != nil {
		return nil, err
	}
	selectNode, ok := statement.(sqlparser.SelectStatement)
	if !ok {
		return nil, fmt.Errorf("only SELECT is supported")
	}
	budget := 100000
	err = sqlparser.Walk(func(node sqlparser.SQLNode) (bool, error) {
		if err := consumeMySQLAnalysisBudget(&budget); err != nil {
			return false, err
		}
		switch n := node.(type) {
		case *sqlparser.AssignmentExpr, *sqlparser.SetVarExpr, *sqlparser.Nextval:
			return false, fmt.Errorf("variable assignments are unsupported")
		case *sqlparser.AliasedTableExpr:
			switch n.Expr.(type) {
			case sqlparser.TableName, *sqlparser.Subquery:
			default:
				return false, fmt.Errorf("unproven table expression")
			}
		case *sqlparser.Select:
			if n.Lock != "" || n.Into != nil {
				return false, fmt.Errorf("locking or SELECT INTO is unsupported")
			}
		case *sqlparser.SetOp:
			if n.Type != sqlparser.UnionAllStr || n.Lock != "" || n.Into != nil {
				return false, fmt.Errorf("only read-only UNION ALL is supported")
			}
		case *sqlparser.FuncExpr:
			if !n.Qualifier.IsEmpty() || n.Over != nil {
				return false, fmt.Errorf("qualified and window functions are unsupported")
			}
			switch strings.ToLower(n.Name.String()) {
			case "count", "coalesce", "date_format", "date_add":
			default:
				return false, fmt.Errorf("unproven function %s", n.Name.String())
			}
		}
		return true, nil
	}, statement)
	if err != nil {
		return nil, err
	}
	if err := expandMySQLCTEs(selectNode, nil, &budget, 0); err != nil {
		return nil, err
	}
	err = sqlparser.Walk(func(sqlparser.SQLNode) (bool, error) {
		return true, consumeMySQLAnalysisBudget(&budget)
	}, statement)
	return statement, err
}

func consumeMySQLAnalysisBudget(remaining *int) error {
	*remaining--
	if *remaining < 0 {
		return fmt.Errorf("query exceeds analysis budget")
	}
	return nil
}

func expandMySQLCTEs(statement sqlparser.SelectStatement, outer map[string]sqlparser.SelectStatement, budget *int, depth int) error {
	if depth > 256 {
		return fmt.Errorf("query exceeds analysis depth budget")
	}

	var with *sqlparser.With
	switch n := statement.(type) {
	case *sqlparser.Select:
		with = n.With
		n.With = nil
	case *sqlparser.SetOp:
		with = n.With
		n.With = nil
	case *sqlparser.ParenSelect:
		return expandMySQLCTEs(n.Select, outer, budget, depth+1)
	default:
		return fmt.Errorf("unsupported query expression")
	}
	env := make(map[string]sqlparser.SelectStatement, len(outer))
	for k, v := range outer {
		env[k] = v
	}
	if with != nil {
		if with.Recursive {
			return fmt.Errorf("recursive CTE is unsupported")
		}
		declared := map[string]bool{}
		for _, cte := range with.Ctes {
			name := strings.ToLower(cte.As.String())
			if name == "" || declared[name] || len(cte.Columns) != 0 {
				return fmt.Errorf("duplicate CTE or explicit CTE column list is unsupported")
			}
			declared[name] = true
			env[name] = nil // self and forward references must not become physical reads
		}
		for _, cte := range with.Ctes {
			sub, ok := cte.Expr.(*sqlparser.Subquery)
			if !ok {
				return fmt.Errorf("CTE requires a SELECT")
			}
			if err := expandMySQLCTEs(sub.Select, env, budget, depth+1); err != nil {
				return err
			}
			env[strings.ToLower(cte.As.String())] = sub.Select
		}
	}
	return sqlparser.Walk(func(node sqlparser.SQLNode) (bool, error) {
		if err := consumeMySQLAnalysisBudget(budget); err != nil {
			return false, err
		}
		if sub, ok := node.(sqlparser.SelectStatement); ok && sub != statement {
			return false, expandMySQLCTEs(sub, env, budget, depth+1)
		}
		if table, ok := node.(*sqlparser.AliasedTableExpr); ok {
			name, ok := table.Expr.(sqlparser.TableName)
			if !ok || !name.DbQualifier.IsEmpty() {
				return true, nil
			}
			if source, exists := env[strings.ToLower(name.Name.String())]; exists {
				if source == nil {
					return false, fmt.Errorf("recursive or forward CTE reference")
				}
				table.Expr = &sqlparser.Subquery{Select: source}
				if table.As.IsEmpty() {
					table.As = name.Name
				}
				return false, nil
			}
		}
		return true, nil
	}, statement)
}
