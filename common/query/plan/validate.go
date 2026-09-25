package plan

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/addp/common/datatype"
)

// Analyze validates the entire plan, including assertion roots, and derives node schemas.
func Analyze(p Plan) (map[NodeID][]datatype.FieldInfo, error) {
	if err := boundedValue(p); err != nil {
		return nil, err
	}
	if p.SchemaVersion != SchemaVersion || p.SemanticProfile != SemanticProfile || len(p.Nodes) == 0 || len(p.Nodes) > MaxNodes || len(p.Parameters) > 128 || len(p.Assertions) > 128 {
		return nil, fmt.Errorf("unsupported or oversized plan")
	}
	v := validator{nodes: map[NodeID]Node{}, schemas: map[NodeID][]datatype.FieldInfo{}, heights: map[NodeID]int{}, visiting: map[NodeID]bool{}, parameters: map[string]Parameter{}, used: map[string]bool{}}
	var err error
	v.parameters, err = validateParameters(p.Parameters)
	if err != nil {
		return nil, err
	}
	for _, n := range p.Nodes {
		if !Symbol(string(n.ID)) {
			return nil, fmt.Errorf("invalid node identity")
		}
		if _, ok := v.nodes[n.ID]; ok {
			return nil, fmt.Errorf("duplicate node identity")
		}
		if err := validateShape(n); err != nil {
			return nil, err
		}
		v.nodes[n.ID] = n
	}
	root, err := v.visit(p.Root, 0)
	if err != nil {
		return nil, err
	}
	assertions := map[Assertion]bool{}
	for _, a := range p.Assertions {
		if !Symbol(a.Code) || assertions[a] {
			return nil, fmt.Errorf("invalid assertion")
		}
		assertions[a] = true
		if _, err := v.visit(a.Violation, 0); err != nil {
			return nil, err
		}
	}
	if len(v.schemas) != len(p.Nodes) || len(v.used) != len(v.parameters) {
		return nil, fmt.Errorf("unreachable nodes or unused parameters")
	}
	if err := validateFields(p.Output.Fields); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(root, p.Output.Fields) {
		return nil, fmt.Errorf("output does not match inferred schema")
	}
	if len(p.Output.StableKey) == 0 {
		return nil, fmt.Errorf("stable key required")
	}
	keys := map[string]bool{}
	for _, k := range p.Output.StableKey {
		found := false
		for _, f := range root {
			if f.Name == k && !f.Nullable {
				found = true
			}
		}
		if !found || keys[k] {
			return nil, fmt.Errorf("invalid stable key")
		}
		keys[k] = true
	}
	sources := map[SourceID][]datatype.FieldInfo{}
	for _, n := range p.Nodes {
		if n.Scan != nil {
			if f, ok := sources[n.Scan.Source]; ok && !reflect.DeepEqual(f, n.Scan.Fields) {
				return nil, fmt.Errorf("source schema conflict")
			}
			sources[n.Scan.Source] = n.Scan.Fields
		}
	}
	raw, err := json.Marshal(p)
	if err != nil || len(raw) > MaxBytes {
		return nil, fmt.Errorf("plan encoding exceeds budget")
	}
	return v.schemas, nil
}
func Validate(p Plan) error { _, err := Analyze(p); return err }

func validateShape(n Node) error {
	payloads := []struct {
		name  string
		value any
	}{
		{"scan", n.Scan}, {"filter", n.Filter}, {"project", n.Project}, {"join", n.Join}, {"distinct", n.Distinct}, {"aggregate", n.Aggregate}, {"union_all", n.UnionAll}, {"constant_rows", n.ConstantRows}, {"date_buckets", n.DateBuckets}, {"sort", n.Sort}, {"limit", n.Limit},
	}
	count := 0
	for _, p := range payloads {
		if !reflect.ValueOf(p.value).IsNil() {
			count++
			if p.name != n.Op {
				return fmt.Errorf("node tag does not match payload")
			}
		}
	}
	if count != 1 {
		return fmt.Errorf("node requires exactly one payload")
	}
	return nil
}
func (v *validator) visit(id NodeID, depth int) ([]datatype.FieldInfo, error) {
	if depth > MaxDepth || v.visiting[id] {
		return nil, fmt.Errorf("cyclic or too deep plan")
	}
	if f, ok := v.schemas[id]; ok {
		if depth+v.heights[id] > MaxDepth {
			return nil, fmt.Errorf("too deep plan")
		}
		return f, nil
	}
	n, ok := v.nodes[id]
	if !ok {
		return nil, fmt.Errorf("unresolved input node")
	}
	v.visiting[id] = true
	scope := map[NodeID][]datatype.FieldInfo{}
	height := 0
	for _, input := range n.Inputs() {
		f, err := v.visit(input, depth+1)
		if err != nil {
			return nil, err
		}
		scope[input] = f
		if v.heights[input]+1 > height {
			height = v.heights[input] + 1
		}
	}
	f, err := v.node(n, scope)
	if err != nil {
		return nil, fmt.Errorf("node %s: %w", id, err)
	}
	if err := validateFields(f); err != nil {
		return nil, fmt.Errorf("node %s: %w", id, err)
	}
	v.visiting[id] = false
	v.schemas[id] = f
	v.heights[id] = height
	return f, nil
}

// Only explicit non-null conjuncts narrow the filter output; the input schema
// and other consumers retain their original nullability.
func refineFilterNullability(e Expr, input NodeID, fields []datatype.FieldInfo) {
	if e.Op == "and" {
		for _, arg := range e.Args {
			refineFilterNullability(arg, input, fields)
		}
		return
	}
	if e.Op != "not" || len(e.Args) != 1 || e.Args[0].Op != "is_null" || len(e.Args[0].Args) != 1 {
		return
	}
	column := e.Args[0].Args[0]
	if column.Op != "column" || column.Column == nil || column.Column.Input != input {
		return
	}
	for i := range fields {
		if fields[i].Name == column.Column.Name {
			fields[i].Nullable = false
		}
	}
}

func (v *validator) projections(p []Projection, scope map[NodeID][]datatype.FieldInfo) ([]datatype.FieldInfo, error) {
	fields := make([]datatype.FieldInfo, 0, len(p))
	for _, c := range p {
		f, err := v.expr(c.Expr, scope, 0)
		if err != nil {
			return nil, err
		}
		f.Name = c.Name
		fields = append(fields, f)
	}
	return fields, nil
}
func (v *validator) node(n Node, scope map[NodeID][]datatype.FieldInfo) ([]datatype.FieldInfo, error) {
	bad := func() ([]datatype.FieldInfo, error) { return nil, fmt.Errorf("invalid node arguments") }
	switch n.Op {
	case "scan":
		if !Symbol(string(n.Scan.Source)) {
			return bad()
		}
		return n.Scan.Fields, nil
	case "filter":
		f, err := v.expr(n.Filter.Predicate, scope, 0)
		if err != nil || f.Type != datatype.FieldTypeBool {
			return bad()
		}
		fields := append([]datatype.FieldInfo(nil), scope[n.Filter.Input]...)
		refineFilterNullability(n.Filter.Predicate, n.Filter.Input, fields)
		return fields, nil
	case "project":
		return v.projections(n.Project.Columns, scope)
	case "join":
		j := n.Join
		if j.Left == j.Right {
			return bad()
		}
		if j.Kind == "cross" {
			if j.On != nil {
				return bad()
			}
		} else if j.Kind == "inner" || j.Kind == "left" {
			if j.On == nil {
				return bad()
			}
			f, err := v.expr(*j.On, scope, 0)
			if err != nil || f.Type != datatype.FieldTypeBool {
				return bad()
			}
		} else {
			return bad()
		}
		out := append([]datatype.FieldInfo(nil), scope[j.Left]...)
		for _, f := range scope[j.Right] {
			if j.Kind == "left" {
				f.Nullable = true
			}
			out = append(out, f)
		}
		return out, nil
	case "distinct":
		return scope[n.Distinct.Input], nil
	case "aggregate":
		a := n.Aggregate
		out, err := v.projections(a.Groups, scope)
		if err != nil {
			return nil, err
		}
		if len(a.Measures) == 0 {
			return bad()
		}
		for _, m := range a.Measures {
			result := field(m.Name, datatype.FieldTypeBigInt, false)
			switch m.Op {
			case "count_rows":
				if m.Value != nil {
					return bad()
				}
			case "count_value":
				if m.Value == nil {
					return bad()
				}
				if _, err := v.expr(*m.Value, scope, 0); err != nil {
					return nil, err
				}
			case "sum_decimal":
				if m.Value == nil {
					return bad()
				}
				value, err := v.expr(*m.Value, scope, 0)
				if err != nil {
					return nil, err
				}
				if value.Type != datatype.FieldTypeDecimal || value.Precision != 38 || value.Scale != 18 {
					return bad()
				}
				result = field(m.Name, datatype.FieldTypeDecimal, true)
				result.Precision, result.Scale = 38, 18
			default:
				return bad()
			}
			out = append(out, result)
		}
		return out, nil
	case "union_all":
		u := n.UnionAll
		if len(u.Inputs) < 2 {
			return bad()
		}
		out := append([]datatype.FieldInfo(nil), scope[u.Inputs[0]]...)
		for _, id := range u.Inputs[1:] {
			f := scope[id]
			if len(f) != len(out) {
				return bad()
			}
			for i, x := range f {
				if x.Type != out[i].Type {
					return bad()
				}
				out[i].Nullable = out[i].Nullable || x.Nullable
			}
		}
		return out, nil
	case "constant_rows":
		c := n.ConstantRows
		if len(c.Rows) > 1024 {
			return bad()
		}
		if err := validateFields(c.Fields); err != nil {
			return nil, err
		}
		for _, row := range c.Rows {
			if len(row) != len(c.Fields) {
				return bad()
			}
			for i, x := range row {
				if _, err := x.Canonical(); err != nil || x.Type != c.Fields[i].Type || (x.Null && !c.Fields[i].Nullable) {
					return bad()
				}
			}
		}
		return c.Fields, nil
	case "date_buckets":
		d := n.DateBuckets
		if d.MaxMonths < 1 || d.MaxMonths > 120 {
			return bad()
		}
		for _, e := range []Expr{d.Start, d.End} {
			f, err := v.expr(e, scope, 0)
			if err != nil || f.Type != datatype.FieldTypeDate || f.Nullable {
				return bad()
			}
		}
		return []datatype.FieldInfo{field(d.Name, datatype.FieldTypeDate, false)}, nil
	case "sort":
		s := n.Sort
		if len(s.Keys) == 0 {
			return bad()
		}
		seen := map[string]bool{}
		for _, k := range s.Keys {
			if seen[k.Name] || (k.Direction != "asc" && k.Direction != "desc") || (k.Nulls != "first" && k.Nulls != "last") {
				return bad()
			}
			seen[k.Name] = true
			found := false
			for _, f := range scope[s.Input] {
				if f.Name == k.Name {
					found = true
				}
			}
			if !found {
				return bad()
			}
		}
		return scope[s.Input], nil
	case "limit":
		if n.Limit.Count < 1 || n.Limit.Count > MaxLimit {
			return bad()
		}
		return scope[n.Limit.Input], nil
	default:
		return bad()
	}
}

func validateParameters(parameters []Parameter) (map[string]Parameter, error) {
	if len(parameters) > MaxParameters {
		return nil, fmt.Errorf("too many parameters")
	}
	v := validator{parameters: map[string]Parameter{}}
	for _, p := range parameters {
		if !Symbol(p.Name) || !supportedType(p.Type) {
			return nil, fmt.Errorf("invalid parameter")
		}
		if _, ok := v.parameters[p.Name]; ok {
			return nil, fmt.Errorf("duplicate parameter")
		}
		seen := map[Literal]bool{}
		if len(p.Allowed) > 100 {
			return nil, fmt.Errorf("too many parameter options")
		}
		for _, a := range p.Allowed {
			c, err := a.Canonical()
			if err != nil || c.Type != p.Type || c.Null || seen[c] {
				return nil, fmt.Errorf("invalid parameter option")
			}
			seen[c] = true
		}
		v.parameters[p.Name] = p
	}
	return v.parameters, nil
}
