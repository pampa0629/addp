package sqlcompile

import (
	"strings"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

// ExpressionDialect is a compiler-internal native strategy, never an owner
// API. Value adds a native type to a trusted expression/parameter; Literal
// safely renders canonical data; Comparable supplies exact comparison keys.
type ExpressionDialect interface {
	plugin.TextPredicateDialect
	ArithmeticDialect
	CalendarDialect
	QuoteIdentifier(string) string
	Value(string, datatype.FieldType) (string, error)
	Literal(plan.Literal) (string, error)
	Comparable(string, datatype.FieldType) (string, error)
	// TrimDecimalText receives fixed-scale decimal text, including a decimal
	// point. ISODateText renders YYYY-MM-DD independently of session settings.
	TrimDecimalText(string) string
	ISODateText(string) string
}

type ExpressionColumn struct{ Relation, Name string }
type ExpressionScope struct {
	Fields     map[plan.NodeID][]datatype.FieldInfo
	Columns    map[plan.ColumnRef]ExpressionColumn
	Parameters []plan.Parameter
}

// CompileExpression validates before rendering, then uses the already tested
// checked scalar components. No separate fixture recursion or engine switch.
func CompileExpression(e plan.Expr, scope ExpressionScope, dialect ExpressionDialect) (CheckedExpression, error) {
	if dialect == nil {
		return CheckedExpression{}, plugin.ErrAnalyticalInvalid
	}
	f, err := plan.AnalyzeExpression(e, scope.Fields, scope.Parameters)
	if err != nil {
		return CheckedExpression{}, plugin.ErrAnalyticalInvalid
	}
	if len(scope.Columns) > plan.MaxExpressions {
		return CheckedExpression{}, plugin.ErrAnalyticalInvalid
	}
	size := 0
	for ref, column := range scope.Columns {
		found := false
		for _, field := range scope.Fields[ref.Input] {
			if field.Name == ref.Name {
				found = true
				break
			}
		}
		if !found {
			return CheckedExpression{}, plugin.ErrAnalyticalInvalid
		}
		for _, name := range []string{column.Relation, column.Name} {
			if name == "" || !utf8.ValidString(name) || strings.ContainsRune(name, 0) || len(name) > plan.MaxBytes-size {
				return CheckedExpression{}, plugin.ErrAnalyticalInvalid
			}
			size += len(name)
		}
	}
	result, err := compileExpression(e, scope, dialect)
	if err != nil {
		return CheckedExpression{}, err
	}
	if result.Type != f.Type {
		return CheckedExpression{}, plugin.ErrAnalyticalInvalid
	}
	return result, nil
}

func compileExpression(e plan.Expr, scope ExpressionScope, d ExpressionDialect) (CheckedExpression, error) {
	result := CheckedExpression{}
	var err error
	switch e.Op {
	case "column":
		binding, ok := scope.Columns[*e.Column]
		if !ok {
			return result, plugin.ErrAnalyticalInvalid
		}
		for _, field := range scope.Fields[e.Column.Input] {
			if field.Name == e.Column.Name {
				result.Type = field.Type
				break
			}
		}
		result.SQL = d.QuoteIdentifier(binding.Relation) + "." + d.QuoteIdentifier(binding.Name)
	case "parameter":
		for _, p := range scope.Parameters {
			if p.Name == e.Parameter {
				result.Type = p.Type
				break
			}
		}
		result.SQL, err = d.Value(":"+e.Parameter, result.Type)
	case "literal":
		value, _ := e.Literal.Canonical() // already validated, canonicalize without mutating the input
		result.Type = value.Type
		result.SQL, err = d.Literal(value)
	default:
		args := make([]CheckedExpression, len(e.Args))
		size := 0
		for i, arg := range e.Args {
			args[i], err = compileExpression(arg, scope, d)
			if err != nil {
				return CheckedExpression{}, err
			}
			size += len(args[i].SQL) + len(args[i].Invalid)
			if size > plan.MaxBytes {
				return CheckedExpression{}, plugin.ErrAnalyticalInvalid
			}

		}
		// Check aggregate input size before composing predicates/SQL strings.
		if err = validateExpressions(args...); err != nil {
			return CheckedExpression{}, err
		}
		switch e.Op {
		case "text":
			return CanonicalText(args[0], d)
		case "date":
			return CalendarDate(args[0], d)
		case "month_start":
			return CalendarMonthStart(args[0], d)
		case "add_months":
			return CalendarAddMonths(args[0], args[1], d)
		case "integer":
			return LosslessInteger(args[0], d)
		case "add", "subtract", "multiply", "divide":
			return Arithmetic(e.Op, args[0], args[1], d)
		case "case":
			return Case(args[0], args[1], args[2])
		case "coalesce":
			return Coalesce(args...)
		case "decimal":
			result.Type = datatype.FieldTypeDecimal
			result.SQL = d.CastDecimal(args[0].SQL, 38, 18)
		case "not", "is_null":
			result.Type = datatype.FieldTypeBool
			if e.Op == "not" {
				result.SQL = "NOT (" + args[0].SQL + ")"
			} else {
				result.SQL = "(" + args[0].SQL + ") IS NULL"
			}
		case "contains":
			result.Type = datatype.FieldTypeBool
			result.SQL = d.Contains(args[0].SQL, args[1].SQL)
		case "and", "or":
			result.Type = datatype.FieldTypeBool
			result.SQL = "(" + args[0].SQL + ") " + strings.ToUpper(e.Op) + " (" + args[1].SQL + ")"
		case "eq", "ne", "lt", "le", "gt", "ge":
			result.Type = datatype.FieldTypeBool
			left, err := d.Comparable(args[0].SQL, args[0].Type)
			if err != nil {
				return CheckedExpression{}, err
			}
			right, err := d.Comparable(args[1].SQL, args[1].Type)
			if err != nil {
				return CheckedExpression{}, err
			}
			op := map[string]string{"eq": "=", "ne": "<>", "lt": "<", "le": "<=", "gt": ">", "ge": ">="}[e.Op]
			result.SQL = "(" + left + ") " + op + " (" + right + ")"
		default:
			return CheckedExpression{}, plugin.ErrAnalyticalUnsupported
		}
		checks := []string{}
		for _, arg := range args {
			if arg.Invalid != "" {
				checks = append(checks, "("+arg.Invalid+")")
			}
		}
		result.Invalid = strings.Join(checks, " OR ")
	}
	if err != nil {
		return CheckedExpression{}, err
	}
	if err = validateExpressions(result); err != nil {
		return CheckedExpression{}, err
	}
	return result, nil
}
