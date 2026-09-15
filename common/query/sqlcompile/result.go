// Package sqlcompile contains SQL compiler components selected explicitly by
// native engine implementations. It never dispatches on engine names.
package sqlcompile

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

// ResultDialect supplies only native syntax. Relation bodies, identifiers and
// order terms are compiler-owned; none of these inputs is an owner SQL escape.
type ResultDialect interface {
	QuoteIdentifier(string) string
	TypedNull(datatype.FieldInfo) (string, error)
	OrderTerms(qualifiedColumn string, field datatype.FieldInfo, key plan.SortKey) ([]string, error)
}

// RenderResult appends a result envelope to an already rendered relation DAG.
// relations maps every node to its compiler-generated CTE identifier. The caller
// supplies the WITH clause. Limits/filters belong inside the root CTE, so an
// empty or paginated root cannot remove the independent assertion records.
// ordering is the order retained by the root relation, including stable keys.
func RenderResult(p plan.Plan, relations map[plan.NodeID]string, ordering []plan.SortKey, dialect ResultDialect) (string, error) {
	layout, err := plugin.NewAnalyticalResultLayout(p)
	if err != nil {
		return "", err
	}
	if dialect == nil || len(relations) != len(p.Nodes) {
		return "", plugin.ErrAnalyticalInvalid
	}
	seen := map[string]bool{}
	for _, node := range p.Nodes {
		name := relations[node.ID]
		if !plan.Symbol(name) || seen[strings.ToLower(name)] {
			return "", plugin.ErrAnalyticalInvalid
		}
		seen[strings.ToLower(name)] = true
	}
	q := dialect.QuoteIdentifier
	columns := make([]string, len(p.Output.Fields))
	nulls := make([]string, len(p.Output.Fields))
	fields := map[string]datatype.FieldInfo{}
	for i, f := range p.Output.Fields {
		columns[i] = q(f.Name)
		fields[f.Name] = f
		nulls[i], err = dialect.TypedNull(f)
		if err != nil {
			return "", err
		}
	}
	control := q(layout.ControlColumn)
	parts := []string{"SELECT " + strings.Join(columns, ", ") + ", 'data' AS " + control + " FROM " + q(relations[p.Root])}
	for i, assertion := range layout.Assertions {
		index := strconv.Itoa(i + 1)
		parts = append(parts, "SELECT "+strings.Join(nulls, ", ")+", CASE WHEN EXISTS (SELECT 1 FROM "+
			q(relations[assertion.Violation])+") THEN 'fail:"+index+"' ELSE 'ok:"+index+"' END AS "+control)
	}
	parts = append(parts, "SELECT "+strings.Join(nulls, ", ")+", 'complete' AS "+control)
	query := "SELECT * FROM (" + strings.Join(parts, " UNION ALL ") + ") AS " + q("__addp_result")
	terms := []string{q("__addp_result") + "." + control + " ASC"}
	ordered := map[string]bool{}
	for _, key := range ordering {
		f, ok := fields[key.Name]
		if !ok || ordered[key.Name] || (key.Direction != "asc" && key.Direction != "desc") || (key.Nulls != "first" && key.Nulls != "last") {
			return "", plugin.ErrAnalyticalInvalid
		}
		ordered[key.Name] = true
		native, err := dialect.OrderTerms(q("__addp_result")+"."+q(key.Name), f, key)
		if err != nil {
			return "", err
		}
		if len(native) == 0 {
			return "", fmt.Errorf("%w: missing result ordering", plugin.ErrAnalyticalUnsupported)
		}
		terms = append(terms, native...)
	}
	query += " ORDER BY " + strings.Join(terms, ", ")
	if len(query) > plan.MaxBytes {
		return "", plugin.ErrAnalyticalInvalid
	}
	return query, nil
}
