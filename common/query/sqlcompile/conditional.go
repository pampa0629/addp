package sqlcompile

import (
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

// CheckedExpression belongs to the native compiler, never the logical plan or
// owner API. SQL must be safe even for invalid data, using an internal NULL
// placeholder if necessary. Invalid is a nonnullable SQL boolean; empty means
// no error is possible. Compose both through enclosing expressions before
// attaching a node-level evaluation check. Publishing a child's unguarded check
// would evaluate inactive branches; dropping a check would hide real failures.
type CheckedExpression struct {
	SQL     string
	Invalid string
	Type    datatype.FieldType
}

func validateExpressions(args ...CheckedExpression) error {
	if len(args) > plan.MaxExpressions {
		return plugin.ErrAnalyticalInvalid
	}
	size := 0
	for _, arg := range args {
		if strings.TrimSpace(arg.SQL) == "" || (arg.Invalid != "" && strings.TrimSpace(arg.Invalid) == "") {
			return plugin.ErrAnalyticalInvalid
		}
		if len(arg.SQL) > plan.MaxBytes-size {
			return plugin.ErrAnalyticalInvalid
		}
		size += len(arg.SQL)
		if len(arg.Invalid) > plan.MaxBytes-size {
			return plugin.ErrAnalyticalInvalid
		}
		size += len(arg.Invalid)
		switch arg.Type {
		case datatype.FieldTypeString, datatype.FieldTypeBool, datatype.FieldTypeInt,
			datatype.FieldTypeBigInt, datatype.FieldTypeDecimal, datatype.FieldTypeDate:
		default:
			return plugin.ErrAnalyticalUnsupported
		}
	}
	return nil
}

func errorPredicate(expr CheckedExpression) string {
	if expr.Invalid == "" {
		return "FALSE"
	}
	return "(" + expr.Invalid + ")"
}

// Case preserves SQL's three-valued condition: NULL selects the else branch.
// An invalid condition always fails, regardless of either branch's value.
func Case(condition, then, otherwise CheckedExpression) (CheckedExpression, error) {
	if err := validateExpressions(condition, then, otherwise); err != nil {
		return CheckedExpression{}, err
	}
	if condition.Type != datatype.FieldTypeBool || then.Type != otherwise.Type {
		return CheckedExpression{}, plugin.ErrAnalyticalInvalid
	}
	result := CheckedExpression{Type: then.Type,
		SQL:     "CASE WHEN (" + condition.SQL + ") THEN (" + then.SQL + ") ELSE (" + otherwise.SQL + ") END",
		Invalid: condition.Invalid,
	}
	if then.Invalid != "" || otherwise.Invalid != "" {
		branch := "CASE WHEN (" + condition.SQL + ") THEN " + errorPredicate(then) + " ELSE " + errorPredicate(otherwise) + " END"
		if result.Invalid != "" {
			branch = "(" + result.Invalid + ") OR (" + branch + ")"
		}
		result.Invalid = branch
	}
	if err := validateExpressions(result); err != nil {
		return CheckedExpression{}, err
	}
	return result, nil
}

// Coalesce tests a reached argument's error before its value. An internal NULL
// placeholder from a failed conversion must never become a successful fallback.
// The flat CASE grows linearly with arguments, without repeating prefix guards.
func Coalesce(args ...CheckedExpression) (CheckedExpression, error) {
	if len(args) < 2 {
		return CheckedExpression{}, plugin.ErrAnalyticalInvalid
	}
	if err := validateExpressions(args...); err != nil {
		return CheckedExpression{}, err
	}
	values := make([]string, len(args))
	lastCheck := -1
	for i, arg := range args {
		if arg.Type != args[0].Type {
			return CheckedExpression{}, plugin.ErrAnalyticalInvalid
		}
		values[i] = "(" + arg.SQL + ")"
		if arg.Invalid != "" {
			lastCheck = i
		}
	}
	result := CheckedExpression{Type: args[0].Type, SQL: "COALESCE(" + strings.Join(values, ", ") + ")"}
	if lastCheck >= 0 {
		var predicate strings.Builder
		predicate.WriteString("CASE")
		for i := 0; i <= lastCheck; i++ {
			if args[i].Invalid != "" {
				predicate.WriteString(" WHEN " + errorPredicate(args[i]) + " THEN TRUE")
			}
			if i < lastCheck {
				predicate.WriteString(" WHEN (" + args[i].SQL + ") IS NOT NULL THEN FALSE")
			}
		}
		predicate.WriteString(" ELSE FALSE END")
		result.Invalid = predicate.String()
	}
	if err := validateExpressions(result); err != nil {
		return CheckedExpression{}, err
	}
	return result, nil
}
