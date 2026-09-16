package sqlcompile

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

const EvaluationFailureCode = "expression_evaluation_failed"

// RenderedRelations is compiler-internal. It is wrapped in CompiledQuery by
// the engine compiler, never passed to an owner as an executable SQL escape.
type RenderedRelations struct {
	SQL         string
	Evaluations []plugin.EvaluationCheck
}

type relationBuilder struct {
	p           plan.Plan
	expression  ExpressionDialect
	result      ResultDialect
	scan        ScanDialect
	sources     map[plan.SourceID]plugin.SourceBinding
	schemas     map[plan.NodeID][]datatype.FieldInfo
	names       map[plan.NodeID]string
	nodes       map[plan.NodeID]plan.Node
	ordering    map[plan.NodeID][]plan.SortKey
	done        map[plan.NodeID]bool
	ctes        []string
	evaluations []EvaluationRelation
	bytes       int
}

// CompileRelations renders supported relational operators through the shared
// scalar compiler. It is not an instance/source certification or a registered
// engine compiler. Native scans require an explicit ScanDialect; date_buckets
// remains unsupported until range compilation is implemented.
func CompileRelations(r plugin.CompileRequest, expression ExpressionDialect, result ResultDialect, scan ScanDialect) (RenderedRelations, error) {
	if expression == nil || result == nil {
		return RenderedRelations{}, plugin.ErrAnalyticalInvalid
	}
	if err := r.Validate(); err != nil {
		return RenderedRelations{}, err
	}
	p := r.Plan
	schemas, err := plan.Analyze(p)
	if err != nil {
		return RenderedRelations{}, fmt.Errorf("%w: %v", plugin.ErrAnalyticalInvalid, err)
	}
	b := &relationBuilder{p: p, expression: expression, result: result, scan: scan, sources: map[plan.SourceID]plugin.SourceBinding{}, schemas: schemas, names: map[plan.NodeID]string{}, nodes: map[plan.NodeID]plan.Node{}, ordering: map[plan.NodeID][]plan.SortKey{}, done: map[plan.NodeID]bool{}}
	for _, source := range r.Sources {
		b.sources[source.Source] = source
	}
	if scan != nil {
		for _, fields := range schemas {
			if err := scan.ValidateSchema(fields); err != nil {
				return RenderedRelations{}, err
			}
		}
	}
	ids := make([]plan.NodeID, 0, len(p.Nodes))
	for _, n := range p.Nodes {
		ids = append(ids, n.ID)
		b.nodes[n.ID] = n
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for i, id := range ids {
		b.names[id] = "r" + strconv.Itoa(i+1)
	}
	for _, id := range ids {
		if err := b.visit(id); err != nil {
			return RenderedRelations{}, fmt.Errorf("node %s: %w", id, err)
		}
	}
	order := append([]plan.SortKey(nil), b.ordering[p.Root]...)
	seen := map[string]bool{}
	for _, key := range order {
		seen[key.Name] = true
	}
	for _, name := range p.Output.StableKey {
		if !seen[name] {
			order = append(order, plan.SortKey{Name: name, Direction: "asc", Nulls: "last"})
		}
	}
	tail, err := RenderResult(p, b.names, order, result, b.evaluations)
	if err != nil {
		return RenderedRelations{}, err
	}
	sql := "WITH " + strings.Join(b.ctes, ", ") + " " + tail
	if len(sql) > plan.MaxBytes {
		return RenderedRelations{}, plugin.ErrAnalyticalInvalid
	}
	out := RenderedRelations{SQL: sql}
	for _, check := range b.evaluations {
		out.Evaluations = append(out.Evaluations, check.Check)
	}
	return out, nil
}
func (b *relationBuilder) q(s string) string { return b.expression.QuoteIdentifier(s) }
func (b *relationBuilder) add(name, query string) error {
	cte := b.q(name) + " AS (" + query + ")"
	if len(cte) > plan.MaxBytes-b.bytes {
		return plugin.ErrAnalyticalInvalid
	}
	b.bytes += len(cte) + 2
	b.ctes = append(b.ctes, cte)
	return nil
}
func (b *relationBuilder) ref(id plan.NodeID) string { return b.q(b.names[id]) }
func (b *relationBuilder) columns(id plan.NodeID) []string {
	out := make([]string, len(b.schemas[id]))
	for i, f := range b.schemas[id] {
		out[i] = b.ref(id) + "." + b.q(f.Name) + " AS " + b.q(f.Name)
	}
	return out
}
func (b *relationBuilder) scope(n plan.Node) ExpressionScope {
	s := ExpressionScope{Fields: map[plan.NodeID][]datatype.FieldInfo{}, Columns: map[plan.ColumnRef]ExpressionColumn{}, Parameters: b.p.Parameters}
	for _, id := range n.Inputs() {
		s.Fields[id] = b.schemas[id]
		for _, f := range b.schemas[id] {
			s.Columns[plan.ColumnRef{Input: id, Name: f.Name}] = ExpressionColumn{Relation: b.names[id], Name: f.Name}
		}
	}
	return s
}
func (b *relationBuilder) ordered(id plan.NodeID, keys []plan.SortKey) (string, error) {
	var terms []string
	for _, key := range keys {
		for _, f := range b.schemas[id] {
			if f.Name == key.Name {
				part, err := b.result.OrderTerms(b.ref(id)+"."+b.q(f.Name), f, key)
				if err != nil {
					return "", err
				}
				if len(part) == 0 {
					return "", plugin.ErrAnalyticalUnsupported
				}
				terms = append(terms, part...)
			}
		}
	}
	if len(terms) == 0 {
		return "", nil
	}
	return " ORDER BY " + strings.Join(terms, ", "), nil
}
func (b *relationBuilder) visit(id plan.NodeID) error {
	if b.done[id] {
		return nil
	}
	n := b.nodes[id]
	for _, input := range n.Inputs() {
		if err := b.visit(input); err != nil {
			return err
		}
	}
	var query string
	var checks []string
	var invalid []string
	scope := b.scope(n)
	expressionBytes := 0
	track := func(x CheckedExpression) error {
		size := len(x.SQL) + len(x.Invalid)
		if size > plan.MaxBytes-expressionBytes {
			return plugin.ErrAnalyticalInvalid
		}
		expressionBytes += size
		return nil
	}
	expr := func(e plan.Expr) (CheckedExpression, error) {
		x, err := CompileExpression(e, scope, b.expression)
		if err == nil {
			err = track(x)
		}
		if err == nil && x.Invalid != "" {
			invalid = append(invalid, "("+x.Invalid+")")
		}
		return x, err
	}
	checkInput := func(from string) {
		if len(invalid) > 0 {
			checks = append(checks, "SELECT 1 AS "+b.q("bad")+" FROM "+from+" WHERE "+strings.Join(invalid, " OR "))
			invalid = nil
		}
	}
	switch n.Op {
	case "scan":
		if b.scan == nil {
			return plugin.ErrAnalyticalUnsupported
		}
		source := b.sources[n.Scan.Source]
		table, err := b.scan.Table(source)
		if err != nil {
			return err
		}
		alias := b.names[id] + "_source"
		from := table + " AS " + b.q(alias)
		bindings := map[string]plugin.ColumnBinding{}
		for _, column := range source.Columns {
			bindings[column.Column] = column
		}
		var columns []string
		for _, field := range n.Scan.Fields {
			x, err := b.scan.Column(bindings[field.Name], alias)
			if err != nil {
				return err
			}
			if x.Type != field.Type {
				return plugin.ErrAnalyticalInvalid
			}
			if err := validateExpressions(x); err != nil {
				return err
			}
			if err := track(x); err != nil {
				return err
			}
			columns = append(columns, x.SQL+" AS "+b.q(field.Name))
			if x.Invalid != "" {
				invalid = append(invalid, "("+x.Invalid+")")
			}
		}
		query = "SELECT " + strings.Join(columns, ", ") + " FROM " + from
		checkInput(from)
	case "constant_rows":
		rows := n.ConstantRows.Rows
		if len(rows) == 0 {
			var columns []string
			for _, f := range n.ConstantRows.Fields {
				v, err := b.result.TypedNull(f)
				if err != nil {
					return err
				}
				columns = append(columns, v+" AS "+b.q(f.Name))
			}
			query = "SELECT " + strings.Join(columns, ", ") + " WHERE FALSE"
		} else {
			var selects []string
			for _, row := range rows {
				var columns []string
				for i, literal := range row {
					v, err := b.expression.Literal(literal)
					if err != nil {
						return err
					}
					columns = append(columns, v+" AS "+b.q(n.ConstantRows.Fields[i].Name))
				}
				selects = append(selects, "SELECT "+strings.Join(columns, ", "))
			}
			query = strings.Join(selects, " UNION ALL ")
		}
	case "filter":
		input := n.Filter.Input
		x, err := expr(n.Filter.Predicate)
		if err != nil {
			return err
		}
		query = "SELECT " + strings.Join(b.columns(input), ", ") + " FROM " + b.ref(input) + " WHERE " + x.SQL
		checkInput(b.ref(input))
		b.ordering[id] = b.ordering[input]
	case "project":
		var columns []string
		mapping := map[string]string{}
		for _, p := range n.Project.Columns {
			x, err := expr(p.Expr)
			if err != nil {
				return err
			}
			columns = append(columns, x.SQL+" AS "+b.q(p.Name))
			if p.Expr.Op == "column" && mapping[p.Expr.Column.Name] == "" {
				mapping[p.Expr.Column.Name] = p.Name
			}
		}
		query = "SELECT " + strings.Join(columns, ", ") + " FROM " + b.ref(n.Project.Input)
		checkInput(b.ref(n.Project.Input))
		for _, key := range b.ordering[n.Project.Input] {
			name := mapping[key.Name]
			if name == "" {
				return plugin.ErrAnalyticalUnsupported
			}
			key.Name = name
			b.ordering[id] = append(b.ordering[id], key)
		}
	case "join":
		j := n.Join
		columns := append(b.columns(j.Left), b.columns(j.Right)...)
		from := b.ref(j.Left) + " CROSS JOIN " + b.ref(j.Right)
		if j.Kind != "cross" {
			on, err := expr(*j.On)
			if err != nil {
				return err
			}
			checkInput(from)
			from = b.ref(j.Left) + " " + strings.ToUpper(j.Kind) + " JOIN " + b.ref(j.Right) + " ON " + on.SQL
		}
		query = "SELECT " + strings.Join(columns, ", ") + " FROM " + from
	case "union_all":
		var selects []string
		for _, input := range n.UnionAll.Inputs {
			var columns []string
			for i, f := range b.schemas[input] {
				columns = append(columns, b.ref(input)+"."+b.q(f.Name)+" AS "+b.q(b.schemas[id][i].Name))
			}
			selects = append(selects, "SELECT "+strings.Join(columns, ", ")+" FROM "+b.ref(input))
		}
		query = strings.Join(selects, " UNION ALL ")
	case "distinct":
		input := n.Distinct.Input
		var columns []string
		for _, f := range b.schemas[input] {
			key, err := b.expression.Comparable(b.ref(input)+"."+b.q(f.Name), f.Type)
			if err != nil {
				return err
			}
			columns = append(columns, key+" AS "+b.q(f.Name))
		}
		keyName := b.names[id] + "_keys"
		if err := b.add(keyName, "SELECT DISTINCT "+strings.Join(columns, ", ")+" FROM "+b.ref(input)); err != nil {
			return err
		}
		columns = nil
		for _, f := range b.schemas[id] {
			value, err := b.expression.Value(b.q(keyName)+"."+b.q(f.Name), f.Type)
			if err != nil {
				return err
			}
			columns = append(columns, value+" AS "+b.q(f.Name))
		}
		query = "SELECT " + strings.Join(columns, ", ") + " FROM " + b.q(keyName)
	case "aggregate":
		a := n.Aggregate
		pre := b.names[id] + "_input"
		agg := b.names[id] + "_groups"
		var preCols, groupKeys, aggCols, output []string
		for i, g := range a.Groups {
			x, err := expr(g.Expr)
			if err != nil {
				return err
			}
			alias := "g" + strconv.Itoa(i)
			preCols = append(preCols, x.SQL+" AS "+b.q(alias))
			key, err := b.expression.Comparable(b.q(pre)+"."+b.q(alias), x.Type)
			if err != nil {
				return err
			}
			groupKeys = append(groupKeys, key)
			aggCols = append(aggCols, key+" AS "+b.q(alias))
			value, err := b.expression.Value(b.q(agg)+"."+b.q(alias), x.Type)
			if err != nil {
				return err
			}
			output = append(output, value+" AS "+b.q(g.Name))
		}
		for i, m := range a.Measures {
			alias := "m" + strconv.Itoa(i)
			value := "*"
			if m.Value != nil {
				x, err := expr(*m.Value)
				if err != nil {
					return err
				}
				preCols = append(preCols, x.SQL+" AS "+b.q(alias))
				value = b.q(pre) + "." + b.q(alias)
			}
			aggCols = append(aggCols, "COUNT("+value+") AS "+b.q(alias))
		}
		checkInput(b.ref(a.Input))
		if len(preCols) == 0 {
			preCols = []string{"1 AS " + b.q("row_marker")}
		}
		if err := b.add(pre, "SELECT "+strings.Join(preCols, ", ")+" FROM "+b.ref(a.Input)); err != nil {
			return err
		}
		aggregateSQL := "SELECT " + strings.Join(aggCols, ", ") + " FROM " + b.q(pre)
		if len(groupKeys) > 0 {
			aggregateSQL += " GROUP BY " + strings.Join(groupKeys, ", ")
		}
		if err := b.add(agg, aggregateSQL); err != nil {
			return err
		}
		for i, m := range a.Measures {
			value := b.expression.CastDecimal(b.q(agg)+"."+b.q("m"+strconv.Itoa(i)), 38, 18)
			x, err := LosslessInteger(CheckedExpression{SQL: value, Type: datatype.FieldTypeDecimal}, b.expression)
			if err != nil {
				return err
			}
			if err := track(x); err != nil {
				return err
			}
			output = append(output, x.SQL+" AS "+b.q(m.Name))
			if x.Invalid != "" {
				invalid = append(invalid, "("+x.Invalid+")")
			}
		}
		checkInput(b.q(agg))
		query = "SELECT " + strings.Join(output, ", ") + " FROM " + b.q(agg)
	case "sort":
		b.ordering[id] = n.Sort.Keys
		order, err := b.ordered(n.Sort.Input, n.Sort.Keys)
		if err != nil {
			return err
		}
		query = "SELECT " + strings.Join(b.columns(n.Sort.Input), ", ") + " FROM " + b.ref(n.Sort.Input) + order
	case "limit":
		input := n.Limit.Input
		keys := b.ordering[input]
		if len(keys) == 0 {
			return plugin.ErrAnalyticalUnsupported
		}
		order, err := b.ordered(input, keys)
		if err != nil {
			return err
		}
		query = "SELECT " + strings.Join(b.columns(input), ", ") + " FROM " + b.ref(input) + order + " LIMIT " + strconv.Itoa(n.Limit.Count)
		b.ordering[id] = keys
	default:
		return plugin.ErrAnalyticalUnsupported
	}
	if err := b.add(b.names[id], query); err != nil {
		return err
	}
	if len(checks) > 0 {
		name := b.names[id] + "_check"
		if err := b.add(name, strings.Join(checks, " UNION ALL ")); err != nil {
			return err
		}
		b.evaluations = append(b.evaluations, EvaluationRelation{Check: plugin.EvaluationCheck{Node: id, Code: EvaluationFailureCode}, Relation: name})
	}
	b.done[id] = true
	return nil
}
