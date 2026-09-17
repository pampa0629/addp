package plan

import (
	"fmt"

	"github.com/addp/common/datatype"
)

type validator struct {
	nodes       map[NodeID]Node
	schemas     map[NodeID][]datatype.FieldInfo
	heights     map[NodeID]int
	visiting    map[NodeID]bool
	parameters  map[string]Parameter
	used        map[string]bool
	expressions int
}

func (v *validator) expr(e Expr, scope map[NodeID][]datatype.FieldInfo, depth int) (datatype.FieldInfo, error) {
	bad := func() (datatype.FieldInfo, error) {
		return datatype.FieldInfo{}, fmt.Errorf("invalid expression shape or types")
	}
	v.expressions++
	if v.expressions > MaxExpressions || depth > MaxDepth {
		return bad()
	}
	leafCount := 0
	if e.Column != nil {
		leafCount++
	}
	if e.Literal != nil {
		leafCount++
	}
	if e.Parameter != "" {
		leafCount++
	}
	if leafCount > 0 {
		if leafCount != 1 || len(e.Args) != 0 {
			return bad()
		}
		switch e.Op {
		case "column":
			if e.Column == nil {
				return bad()
			}
			for _, f := range scope[e.Column.Input] {
				if f.Name == e.Column.Name {
					f.Name = ""
					return f, nil
				}
			}
			return bad()
		case "parameter":
			p, ok := v.parameters[e.Parameter]
			if !ok {
				return bad()
			}
			v.used[p.Name] = true
			return field("", p.Type, !p.Required), nil
		case "literal":
			if e.Literal == nil {
				return bad()
			}
			if _, err := e.Literal.Canonical(); err != nil {
				return bad()
			}
			return field("", e.Literal.Type, e.Literal.Null), nil
		default:
			return bad()
		}
	}
	if len(e.Args) > MaxExpressions-v.expressions {
		return bad()
	}
	args := make([]datatype.FieldInfo, len(e.Args))
	nullable := false
	for i, a := range e.Args {
		f, err := v.expr(a, scope, depth+1)
		if err != nil {
			return bad()
		}
		args[i] = f
		nullable = nullable || f.Nullable
	}
	numeric := func(t datatype.FieldType) bool {
		return t == datatype.FieldTypeInt || t == datatype.FieldTypeBigInt || t == datatype.FieldTypeDecimal
	}
	same := func() bool {
		for _, a := range args {
			if a.Type != args[0].Type {
				return false
			}
		}
		return true
	}
	switch e.Op {
	case "contains":
		if len(args) != 2 || !same() || args[0].Type != datatype.FieldTypeString {
			return bad()
		}
		return field("", datatype.FieldTypeBool, nullable), nil
	case "eq", "ne", "lt", "le", "gt", "ge":
		if len(args) != 2 || !same() {
			return bad()
		}
		return field("", datatype.FieldTypeBool, nullable), nil
	case "and", "or":
		if len(args) != 2 || !same() || args[0].Type != datatype.FieldTypeBool {
			return bad()
		}
		return field("", datatype.FieldTypeBool, nullable), nil
	case "not":
		if len(args) != 1 || args[0].Type != datatype.FieldTypeBool {
			return bad()
		}
		return args[0], nil
	case "is_null":
		if len(args) != 1 {
			return bad()
		}
		return field("", datatype.FieldTypeBool, false), nil
	case "add", "subtract", "multiply", "divide":
		if len(args) != 2 || !numeric(args[0].Type) || !numeric(args[1].Type) {
			return bad()
		}
		t := datatype.FieldTypeBigInt
		if e.Op == "divide" || args[0].Type == datatype.FieldTypeDecimal || args[1].Type == datatype.FieldTypeDecimal {
			t = datatype.FieldTypeDecimal
		}
		return field("", t, nullable), nil
	case "case":
		if len(args) != 3 || args[0].Type != datatype.FieldTypeBool || args[1].Type != args[2].Type {
			return bad()
		}
		f := args[1]
		f.Nullable = args[1].Nullable || args[2].Nullable
		return f, nil
	case "coalesce":
		if len(args) < 2 || !same() {
			return bad()
		}
		f := args[0]
		f.Nullable = true
		for _, a := range args {
			f.Nullable = f.Nullable && a.Nullable
		}
		return f, nil
	case "date":
		if len(args) != 1 || (args[0].Type != datatype.FieldTypeString && args[0].Type != datatype.FieldTypeDate) {
			return bad()
		}
		return field("", datatype.FieldTypeDate, nullable), nil
	case "month_start":
		if len(args) != 1 || args[0].Type != datatype.FieldTypeDate {
			return bad()
		}
		return args[0], nil
	case "add_months":
		if len(args) != 2 || args[0].Type != datatype.FieldTypeDate || (args[1].Type != datatype.FieldTypeInt && args[1].Type != datatype.FieldTypeBigInt) {
			return bad()
		}
		return field("", datatype.FieldTypeDate, nullable), nil
	case "decimal", "integer":
		if len(args) != 1 || !numeric(args[0].Type) {
			return bad()
		}
		t := datatype.FieldTypeDecimal
		if e.Op == "integer" {
			t = datatype.FieldTypeBigInt
		}
		return field("", t, nullable), nil
	case "text":
		if len(args) != 1 {
			return bad()
		}
		return field("", datatype.FieldTypeString, nullable), nil
	default:
		return bad()
	}
}
