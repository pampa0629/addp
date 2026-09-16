package postgresql

type analyticalIntegerDialect struct{}

func (analyticalIntegerDialect) TruncateDecimal(value string) string {
	return "pg_catalog.trunc(" + value + ", 0)"
}

func (analyticalIntegerDialect) CastInteger(value string) string {
	return "CAST(" + value + " AS bigint)"
}
