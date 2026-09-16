package conformance

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/query/plan"
)

type ArithmeticCase struct {
	Name, Op    string
	Left, Right plan.Literal
}

// ArithmeticCases has no native SQL or engine expectations. A rational oracle
// supplies the independent result, with one rounding at each logical operation.
func ArithmeticCases() []ArithmeticCase {
	var cases []ArithmeticCase
	add := func(op string, typ datatype.FieldType, pairs ...[2]string) {
		for _, pair := range pairs {
			literal := func(s string) plan.Literal {
				if s == "null" {
					return plan.Literal{Type: typ, Null: true}
				}
				return plan.Literal{Type: typ, Text: s}
			}
			cases = append(cases, ArithmeticCase{fmt.Sprintf("%s/%s/%s/%s", op, typ, pair[0], pair[1]), op, literal(pair[0]), literal(pair[1])})
		}
	}
	const max = "99999999999999999999.999999999999999999"
	const unit = "0.000000000000000001"
	add("add", datatype.FieldTypeBigInt, [2]string{"9007199254740993", "1"}, [2]string{"9223372036854775807", "1"}, [2]string{"-9223372036854775808", "-1"}, [2]string{"null", "1"})
	add("subtract", datatype.FieldTypeBigInt, [2]string{"-9223372036854775808", "1"}, [2]string{"9223372036854775807", "-9223372036854775808"}, [2]string{"-9223372036854775808", "-1"})
	add("multiply", datatype.FieldTypeBigInt, [2]string{"-9223372036854775808", "-1"}, [2]string{"9223372036854775807", "9223372036854775807"}, [2]string{"-9223372036854775808", "0"}, [2]string{"-3", "7"})
	add("divide", datatype.FieldTypeBigInt, [2]string{"1", "2"}, [2]string{"1", "0"}, [2]string{"-9223372036854775808", "-1"})
	add("add", datatype.FieldTypeDecimal, [2]string{max, unit}, [2]string{max, "-" + max}, [2]string{"-" + max, "-" + unit}, [2]string{"1.25", "2.5"})
	add("subtract", datatype.FieldTypeDecimal, [2]string{max, "-" + unit}, [2]string{max, max}, [2]string{"0", unit})
	add("multiply", datatype.FieldTypeDecimal, [2]string{unit, "0.5"}, [2]string{"-" + unit, "0.5"}, [2]string{unit, "0.499999999999999999"}, [2]string{unit, "0.500000000000000001"}, [2]string{max, max}, [2]string{max, "1"}, [2]string{max, "0"}, [2]string{"10000000000000000000", unit}, [2]string{"1.000000000000000001", "0.5"}, [2]string{"1.000000000000000001", "0.499999999999999999"}, [2]string{"1.000000000000000001", "1.000000000000000001"}, [2]string{"null", max})
	add("divide", datatype.FieldTypeDecimal, [2]string{"1", "3"}, [2]string{"2", "3"}, [2]string{"-2", "3"}, [2]string{"2", "-3"}, [2]string{unit, "2"}, [2]string{"-" + unit, "2"}, [2]string{unit, "2.000000000000000001"}, [2]string{max, unit}, [2]string{max, "1"}, [2]string{unit, max}, [2]string{"1", max}, [2]string{"0", "0"}, [2]string{"null", "0"}, [2]string{"1", "null"}, [2]string{"0", max}, [2]string{"0.499999999999999999", "0.999999999999999999"}, [2]string{"-0.499999999999999999", "0.999999999999999999"}, [2]string{"0.500000000000000001", "1.000000000000000003"}, [2]string{max, "0.999999999999999999"})
	add("multiply", datatype.FieldTypeDecimal,
		[2]string{"9999999999.999999999999999999", "10000000000.000000000000000001"},
		[2]string{"-9999999999.999999999999999999", "10000000000.000000000000000001"},
		[2]string{"0.999999999999999999", "1.000000000000000001"})
	// Explicit mixed types exercise logical promotion instead of engine defaults.
	cases = append(cases, ArithmeticCase{"mixed integer decimal", "add", plan.Literal{Type: datatype.FieldTypeInt, Text: "2"}, plan.Literal{Type: datatype.FieldTypeDecimal, Text: "0.25"}})
	return cases
}

func (c ArithmeticCase) OutputType() datatype.FieldType {
	if c.Op == "divide" || c.Left.Type == datatype.FieldTypeDecimal || c.Right.Type == datatype.FieldTypeDecimal {
		return datatype.FieldTypeDecimal
	}
	return datatype.FieldTypeBigInt
}

// Check compares a native result against rational arithmetic, never float64 or
// another database. keep=false still checks errors, without expecting data rows.
func (c ArithmeticCase) Check(t *testing.T, value *string, invalid, keep bool, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	var want *big.Rat
	wantInvalid := false
	if !c.Left.Null && !c.Right.Null {
		a, aok := new(big.Rat).SetString(c.Left.Text)
		b, bok := new(big.Rat).SetString(c.Right.Text)
		if !aok || !bok {
			t.Fatal("invalid fixture literal")
		}
		want = new(big.Rat)
		switch c.Op {
		case "add":
			want.Add(a, b)
		case "subtract":
			want.Sub(a, b)
		case "multiply":
			want.Mul(a, b)
		case "divide":
			if b.Sign() == 0 {
				wantInvalid = true
			} else {
				want.Quo(a, b)
			}
		}
		if c.OutputType() == datatype.FieldTypeDecimal {
			factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
			num := new(big.Int).Mul(new(big.Int).Abs(want.Num()), factor)
			q, r := new(big.Int), new(big.Int)
			q.QuoRem(num, want.Denom(), r)
			if new(big.Int).Lsh(r, 1).Cmp(want.Denom()) >= 0 {
				q.Add(q, big.NewInt(1))
			}
			bound := new(big.Int).Exp(big.NewInt(10), big.NewInt(38), nil)
			wantInvalid = wantInvalid || q.Cmp(bound) >= 0
			if want.Sign() < 0 {
				q.Neg(q)
			}
			want.SetFrac(q, factor)
		} else {
			wantInvalid = !want.IsInt() || !want.Num().IsInt64()
		}
	}
	if invalid != wantInvalid {
		t.Fatalf("invalid=%t want=%t value=%v", invalid, wantInvalid, value)
	}
	if wantInvalid || want == nil || !keep {
		if value != nil {
			t.Fatalf("expected no value, got %s", *value)
		}
		return
	}
	if value == nil {
		t.Fatalf("expected %s, got NULL", want.RatString())
	}
	// Also reject a numerically equal representation outside the output contract.
	if _, err := (plan.Literal{Type: c.OutputType(), Text: *value}).Canonical(); err != nil {
		t.Fatal(err)
	}
	got, ok := new(big.Rat).SetString(*value)
	if !ok || got.Cmp(want) != 0 {
		t.Fatalf("got %s want %s", *value, want.FloatString(18))
	}
}
