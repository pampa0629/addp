package sqlcompile

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

// ArithmeticDialect supplies the exact decimal workspace used by this SQL
// strategy: 65 integer digits, casts through scale 30, and an estimate of a
// positive quotient below 10^20 with absolute error <= 10^-30. Estimates never
// decide rounding: the exact scaled-integer residual does. Engines unable to
// meet these requirements must use a different certified compiler strategy.
type ArithmeticDialect interface {
	IntegerDialect
	CastDecimal(value string, precision, scale int) string
	QuotientEstimate(numerator, denominator string) string
}

const (
	decimalUnit  = "0.000000000000000001"
	decimalScale = "1000000000000000000"
	decimalLimit = "100000000000000000000"
	decimalMax   = "99999999999999999999.999999999999999999"
	scaledMax    = "99999999999999999999999999999999999999"
)

// Stop expanding as soon as an intermediate fragment exceeds the common
// budget; checking only the final string could allocate hundreds of megabytes.
type scalarBuilder struct{ failed bool }

func (b *scalarBuilder) fits(extra int, fragments ...string) bool {
	if b.failed {
		return false
	}
	size := extra
	for _, s := range fragments {
		if len(s) > plan.MaxBytes-size {
			b.failed = true
			return false
		}
		size += len(s)
	}
	return true
}
func (b *scalarBuilder) binary(a, op, c string) string {
	if !b.fits(4, a, op, c) {
		return ""
	}
	return "(" + a + " " + op + " " + c + ")"
}
func (b *scalarBuilder) choose(condition, yes, no string) string {
	if !b.fits(40, condition, yes, no) {
		return ""
	}
	return "CASE WHEN " + condition + " THEN (" + yes + ") ELSE (" + no + ") END"
}
func (b *scalarBuilder) absolute(value string) string {
	return b.choose(b.binary(value, "<", "0"), "-("+value+")", value)
}

// Arithmetic compiles a total value/error pair. Both operands are evaluated;
// only CASE/COALESCE may mask a child's error. Numeric failures share this
// expression's evaluation check, independently of the final business rows.
func Arithmetic(op string, left, right CheckedExpression, dialect ArithmeticDialect) (CheckedExpression, error) {
	if dialect == nil {
		return CheckedExpression{}, plugin.ErrAnalyticalInvalid
	}
	if err := validateExpressions(left, right); err != nil {
		return CheckedExpression{}, err
	}
	isInteger := func(t datatype.FieldType) bool { return t == datatype.FieldTypeInt || t == datatype.FieldTypeBigInt }
	if (!isInteger(left.Type) && left.Type != datatype.FieldTypeDecimal) || (!isInteger(right.Type) && right.Type != datatype.FieldTypeDecimal) {
		return CheckedExpression{}, plugin.ErrAnalyticalUnsupported
	}
	symbol := map[string]string{"add": "+", "subtract": "-", "multiply": "*", "divide": "/"}[op]
	if symbol == "" {
		return CheckedExpression{}, plugin.ErrAnalyticalUnsupported
	}
	build := &scalarBuilder{}
	binary, choose, absolute := build.binary, build.choose, build.absolute
	work := func(value string) string { return dialect.CastDecimal(value, 65, 0) }
	nonnull := "(" + left.SQL + ") IS NOT NULL AND (" + right.SQL + ") IS NOT NULL"
	result := CheckedExpression{Type: datatype.FieldTypeDecimal}
	var invalid string
	if isInteger(left.Type) && isInteger(right.Type) && op != "divide" {
		// Products of two int64 values fit within 38 digits. Widen BEFORE the
		// operation, then guard the narrowing cast to avoid native overflow.
		value := binary(work(left.SQL), symbol, work(right.SQL))
		valid := binary(value, ">=", "-9223372036854775808") + " AND " + binary(value, "<=", "9223372036854775807")
		result.Type = datatype.FieldTypeBigInt
		result.SQL = choose(valid, dialect.CastInteger(value), dialect.CastInteger("NULL"))
		invalid = "(" + nonnull + ") AND NOT (" + valid + ")"
	} else if op == "add" || op == "subtract" {
		// At most 21 integer and 18 fractional digits: no intermediate loss.
		value := binary(dialect.CastDecimal(left.SQL, 38, 18), symbol, dialect.CastDecimal(right.SQL, 38, 18))
		valid := binary(value, ">=", "-"+decimalMax) + " AND " + binary(value, "<=", decimalMax)
		result.SQL = choose(valid, dialect.CastDecimal(value, 38, 18), dialect.CastDecimal("NULL", 38, 18))
		invalid = "(" + nonnull + ") AND NOT (" + valid + ")"
	} else {
		x, y := dialect.CastDecimal(left.SQL, 38, 18), dialect.CastDecimal(right.SQL, 38, 18)
		a, b := absolute(x), absolute(y)
		// A and B are nonnegative integer coefficients of decimal(38,18).
		A, B := work(binary(a, "*", decimalScale)), work(binary(b, "*", decimalScale))
		var rounded, preinvalid string
		if op == "multiply" {
			// A*B may need 76 digits. Split B into whole/fractional parts:
			// A*whole(B) needs <=58 digits and A*fraction(B) <=56 digits.
			whole := work(dialect.TruncateDecimal(b))
			fraction := work(binary(B, "%", decimalScale))
			low := binary(A, "*", fraction)
			remainder := binary(low, "%", decimalScale)
			quotient := work(binary(binary(low, "-", remainder), "*", decimalUnit))
			units := binary(binary(A, "*", whole), "+", quotient)
			roundUp := choose(binary(remainder, ">=", "500000000000000000"), "1", "0")
			rounded = binary(units, "+", roundUp)
		} else {
			// Exclude zero and definite overflow before native division. The
			// remaining quotient is <10^20; widening the numerator's scale
			// avoids dependence on a connection's default division precision.
			tooLarge := binary(a, ">=", binary(b, "*", decimalLimit))
			allowed := binary(b, ">", "0") + " AND NOT (" + tooLarge + ")"
			safeA := choose(allowed, a, dialect.CastDecimal("NULL", 38, 18))
			safeB := choose(binary(b, ">", "0"), b, dialect.CastDecimal("NULL", 38, 18))
			estimate := dialect.QuotientEstimate(safeA, safeB)
			// 39,18 permits rounding a near-limit estimate up to 10^20;
			// the final result guard must diagnose that instead of the cast.
			q := work(binary(dialect.CastDecimal(estimate, 39, 18), "*", decimalScale))
			r := binary(binary(A, "*", decimalScale), "-", binary(q, "*", B))
			twiceR := binary(r, "*", "2")
			// q is within one unit of the exact scaled quotient. Positive
			// ties increase q; negative residual ties keep q (away from zero).
			correction := choose(binary(twiceR, ">=", B), "1", choose(binary(twiceR, "<", "-("+B+")"), "-1", "0"))
			rounded = binary(q, "+", correction)
			preinvalid = binary(b, "=", "0") + " OR " + tooLarge
		}
		valid := binary(rounded, "<=", scaledMax)
		value := binary(rounded, "*", decimalUnit)
		signed := choose(binary(binary(x, "<", "0"), "<>", binary(y, "<", "0")), "-("+value+")", value)
		result.SQL = choose(valid, dialect.CastDecimal(signed, 38, 18), dialect.CastDecimal("NULL", 38, 18))
		if preinvalid != "" {
			invalid = preinvalid + " OR NOT (" + valid + ")"
		} else {
			invalid = "NOT (" + valid + ")"
		}
		invalid = "(" + nonnull + ") AND (" + invalid + ")"
	}
	result.Invalid = invalid
	for _, input := range []CheckedExpression{left, right} {
		if input.Invalid != "" {
			result.Invalid = binary(input.Invalid, "OR", result.Invalid)
		}
	}
	if build.failed {
		return CheckedExpression{}, plugin.ErrAnalyticalInvalid
	}
	if err := validateExpressions(result); err != nil {
		return CheckedExpression{}, err
	}
	return result, nil
}
