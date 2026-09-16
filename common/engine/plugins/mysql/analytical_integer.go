package mysql

type analyticalIntegerDialect struct{}

func (analyticalIntegerDialect) TruncateDecimal(value string) string {
	return "TRUNCATE(" + value + ", 0)"
}

func (analyticalIntegerDialect) CastInteger(value string) string {
	return "CAST(" + value + " AS signed)"
}
