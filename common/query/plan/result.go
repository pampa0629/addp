package plan

import (
	"fmt"

	"github.com/addp/common/datatype"
)

// ResultRequest operates on a completed result, never on its source relations.
// After contains decoded cursor values in the complete effective OrderBy order.
// Limit is the requested page size; one extra row is retained for the caller.
type ResultRequest struct {
	Select  []string
	Filter  *ResultFilter
	OrderBy []SortKey
	After   []Literal
	Limit   int
}

// ResultFilter is a closed union: and/or/not have Children only; comparison,
// in, is_null and not_null have Field and the appropriate number of Values.
type ResultFilter struct {
	Op       string
	Field    string
	Values   []Literal
	Children []ResultFilter
}

// ResultPlan keeps runtime values separate from the plan. HiddenFields remain
// in the execution output for cursor construction and are removed by the owner.
type ResultPlan struct {
	Plan           Plan
	Values         map[string]Literal
	SelectedFields []string
	HiddenFields   []string
	OrderBy        []SortKey
}

// ApplyResultRequest copies the frozen plan and preserves every assertion.
// It adds no SQL, physical catalog assumptions or engine-dependent behavior.
func ApplyResultRequest(base Plan, request ResultRequest) (ResultPlan, error) {
	if err := boundedValue(request); err != nil {
		return ResultPlan{}, err
	}
	if request.Limit < 1 || request.Limit >= MaxLimit {
		return ResultPlan{}, fmt.Errorf("invalid result page size")
	}
	raw, err := CanonicalJSON(base)
	if err != nil {
		return ResultPlan{}, err
	}
	p, err := Decode(raw)
	if err != nil {
		return ResultPlan{}, err
	}
	b := resultBuilder{p: p, fields: map[string]datatype.FieldInfo{}, names: map[string]bool{}, values: map[string]Literal{}}
	for _, f := range p.Output.Fields {
		b.fields[f.Name] = f
	}
	for _, n := range p.Nodes {
		b.names[string(n.ID)] = true
	}
	for _, param := range p.Parameters {
		b.names[param.Name] = true
	}
	selected := append([]string(nil), request.Select...)
	if len(selected) == 0 {
		for _, f := range p.Output.Fields {
			selected = append(selected, f.Name)
		}
	}
	seen := map[string]bool{}
	for _, name := range selected {
		if _, ok := b.fields[name]; !ok || seen[name] {
			return ResultPlan{}, fmt.Errorf("invalid result selection")
		}
		seen[name] = true
	}
	order := append([]SortKey(nil), request.OrderBy...)
	ordered := map[string]bool{}
	for i := range order {
		key := &order[i]
		f, ok := b.fields[key.Name]
		if !ok || f.Nullable || ordered[key.Name] || (key.Direction != "asc" && key.Direction != "desc") || (key.Nulls != "" && key.Nulls != "first" && key.Nulls != "last") {
			return ResultPlan{}, fmt.Errorf("invalid result ordering")
		}
		// All permitted sort fields are non-nullable.
		key.Nulls = "last"
		ordered[key.Name] = true
	}
	for _, name := range p.Output.StableKey {
		if !ordered[name] {
			order = append(order, SortKey{Name: name, Direction: "asc", Nulls: "last"})
		}
	}
	var hidden []string
	for _, key := range order {
		if !seen[key.Name] {
			hidden = append(hidden, key.Name)
			seen[key.Name] = true
		}
	}
	if request.Filter != nil {
		predicate, err := b.filter(*request.Filter, 0)
		if err != nil {
			return ResultPlan{}, err
		}
		b.addFilter(predicate)
	}
	if len(request.After) > 0 {
		if len(request.After) != len(order) || len(order)*len(order)*4 > MaxExpressions {
			return ResultPlan{}, fmt.Errorf("invalid result cursor size")
		}
		params := make([]Expr, len(order))
		for i, key := range order {
			params[i], err = b.parameter(key.Name, request.After[i])
			if err != nil {
				return ResultPlan{}, err
			}
		}
		var alternatives []Expr
		for i, key := range order {
			var terms []Expr
			for j := 0; j < i; j++ {
				terms = append(terms, resultOp("eq", b.column(order[j].Name), params[j]))
			}
			comparison := "gt"
			if key.Direction == "desc" {
				comparison = "lt"
			}
			terms = append(terms, resultOp(comparison, b.column(key.Name), params[i]))
			alternatives = append(alternatives, resultBoolean("and", terms))
		}
		b.addFilter(resultBoolean("or", alternatives))
	}
	sort := NodeID(b.name())
	b.p.Nodes = append(b.p.Nodes, Node{ID: sort, Op: "sort", Sort: &Sort{Input: b.p.Root, Keys: append([]SortKey(nil), order...)}})
	limit := NodeID(b.name())
	b.p.Nodes = append(b.p.Nodes, Node{ID: limit, Op: "limit", Limit: &Limit{Input: sort, Count: request.Limit + 1}})
	b.p.Root = limit
	columns := make([]Projection, 0, len(selected)+len(hidden))
	fields := make([]datatype.FieldInfo, 0, cap(columns))
	for _, name := range append(append([]string(nil), selected...), hidden...) {
		columns = append(columns, Projection{Name: name, Expr: b.column(name)})
		fields = append(fields, b.fields[name])
	}
	project := NodeID(b.name())
	b.p.Nodes = append(b.p.Nodes, Node{ID: project, Op: "project", Project: &Project{Input: b.p.Root, Columns: columns}})
	b.p.Root = project
	b.p.Output.Fields = fields
	if err := Validate(b.p); err != nil {
		return ResultPlan{}, err
	}
	return ResultPlan{Plan: b.p, Values: b.values, SelectedFields: selected, HiddenFields: hidden, OrderBy: order}, nil
}

type resultBuilder struct {
	p      Plan
	fields map[string]datatype.FieldInfo
	names  map[string]bool
	values map[string]Literal
	next   int
	terms  int
}

func (b *resultBuilder) name() string {
	for {
		name := fmt.Sprintf("result_%d", b.next)
		b.next++
		if !b.names[name] {
			b.names[name] = true
			return name
		}
	}
}

func (b *resultBuilder) column(name string) Expr {
	return Expr{Op: "column", Column: &ColumnRef{Input: b.p.Root, Name: name}}
}

func (b *resultBuilder) parameter(field string, value Literal) (Expr, error) {
	f, ok := b.fields[field]
	if !ok || value.Type != f.Type || value.Null {
		return Expr{}, fmt.Errorf("invalid result comparison value")
	}
	value, err := value.Canonical()
	if err != nil {
		return Expr{}, err
	}
	if len(b.p.Parameters) >= MaxParameters {
		return Expr{}, fmt.Errorf("too many result parameters")
	}
	name := b.name()
	b.p.Parameters = append(b.p.Parameters, Parameter{Name: name, Type: f.Type, Required: true})
	b.values[name] = value
	return Expr{Op: "parameter", Parameter: name}, nil
}

func (b *resultBuilder) addFilter(predicate Expr) {
	fields := make([]datatype.FieldInfo, 0, len(b.fields))
	for _, field := range b.fields {
		fields = append(fields, field)
	}
	refineFilterNullability(predicate, b.p.Root, fields)
	for _, field := range fields {
		b.fields[field.Name] = field
	}
	id := NodeID(b.name())
	b.p.Nodes = append(b.p.Nodes, Node{ID: id, Op: "filter", Filter: &Filter{Input: b.p.Root, Predicate: predicate}})
	b.p.Root = id
}

func (b *resultBuilder) filter(f ResultFilter, depth int) (Expr, error) {
	b.terms++
	if depth > MaxDepth/2 || b.terms > MaxExpressions/4 {
		return Expr{}, fmt.Errorf("result filter exceeds budget")
	}
	if f.Op == "and" || f.Op == "or" || f.Op == "not" {
		if f.Field != "" || len(f.Values) != 0 || len(f.Children) == 0 || (f.Op == "not" && len(f.Children) != 1) {
			return Expr{}, fmt.Errorf("invalid result boolean filter")
		}
		children := make([]Expr, len(f.Children))
		for i, child := range f.Children {
			var err error
			children[i], err = b.filter(child, depth+1)
			if err != nil {
				return Expr{}, err
			}
		}
		if f.Op == "not" {
			return resultOp("not", children[0]), nil
		}
		return resultBoolean(f.Op, children), nil
	}
	if _, ok := b.fields[f.Field]; !ok || len(f.Children) != 0 {
		return Expr{}, fmt.Errorf("invalid result filter field")
	}
	column := b.column(f.Field)
	switch f.Op {
	case "is_null", "not_null":
		if len(f.Values) != 0 {
			return Expr{}, fmt.Errorf("null filter does not accept values")
		}
		e := resultOp("is_null", column)
		if f.Op == "not_null" {
			e = resultOp("not", e)
		}
		return e, nil
	case "eq", "ne", "lt", "le", "gt", "ge", "in", "contains":
		if len(f.Values) == 0 || (f.Op != "in" && len(f.Values) != 1) || len(f.Values) > MaxParameters {
			return Expr{}, fmt.Errorf("invalid result comparison arity")
		}
		comparisons := make([]Expr, len(f.Values))
		for i, value := range f.Values {
			param, err := b.parameter(f.Field, value)
			if err != nil {
				return Expr{}, err
			}
			op := f.Op
			if op == "in" {
				op = "eq"
			}
			comparisons[i] = resultOp(op, column, param)
		}
		return resultBoolean("or", comparisons), nil
	default:
		return Expr{}, fmt.Errorf("unsupported result filter operation")
	}
}

func resultOp(op string, args ...Expr) Expr { return Expr{Op: op, Args: args} }

// Balance expansions so a long IN list does not create a linear-depth tree.
func resultBoolean(op string, args []Expr) Expr {
	if len(args) == 1 {
		return args[0]
	}
	mid := len(args) / 2
	return resultOp(op, resultBoolean(op, args[:mid]), resultBoolean(op, args[mid:]))
}
