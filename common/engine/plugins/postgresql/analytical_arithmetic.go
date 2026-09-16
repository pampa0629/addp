package postgresql

import "fmt"

type analyticalArithmeticDialect struct{ analyticalIntegerDialect }

func (analyticalArithmeticDialect) CastDecimal(value string, precision, scale int) string {
	return fmt.Sprintf("CAST(%s AS decimal(%d,%d))", value, precision, scale)
}

func (d analyticalArithmeticDialect) QuotientEstimate(numerator, denominator string) string {
	return "(" + d.CastDecimal(numerator, 50, 30) + " / " + d.CastDecimal(denominator, 38, 18) + ")"
}
