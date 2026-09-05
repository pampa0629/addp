package query

import (
	"fmt"
	"strings"
)

const (
	DialectPostgreSQL = "postgresql"
	DialectOracle     = "oracle"
	DialectMySQL      = "mysql"
	DialectClickHouse = "clickhouse"
	DialectSparkSQL   = "spark_sql"

	doubleQuote = `"`
	backtick    = "`"
)

type Dialect struct {
	name  string
	quote string
}

func ForDialect(name string) Dialect {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case DialectMySQL, DialectClickHouse, DialectSparkSQL:
		return Dialect{name: normalized, quote: backtick}
	default:
		return Dialect{name: normalized, quote: doubleQuote}
	}
}

func (d Dialect) Name() string {
	return d.name
}

func (d Dialect) IsPostgreSQL() bool {
	return d.name == DialectPostgreSQL
}

func (d Dialect) IsOracle() bool {
	return d.name == DialectOracle
}

func (d Dialect) QuoteIdentifier(identifier string) string {
	quote := d.quote
	if quote == "" {
		quote = doubleQuote
	}
	return quote + strings.ReplaceAll(identifier, quote, quote+quote) + quote
}

// Placeholder returns the driver placeholder for a one-based argument
// position. Callers that already have bound arguments must pass the next
// position so numbered dialects continue the existing sequence.
func (d Dialect) Placeholder(position int) string {
	switch SQLPlaceholderStyleForDialect(d.name) {
	case SQLPlaceholderDollar:
		return fmt.Sprintf("$%d", position)
	case SQLPlaceholderColon:
		return fmt.Sprintf(":%d", position)
	default:
		return "?"
	}
}

func (d Dialect) QualifiedTable(namespace, table string) string {
	if strings.TrimSpace(namespace) == "" {
		return d.QuoteIdentifier(table)
	}
	return d.QuoteIdentifier(namespace) + "." + d.QuoteIdentifier(table)
}

func (d Dialect) SelectTableSQL(selectExpr, namespace, table, whereClause, orderByClause string, limit, offset int) string {
	selectExpr = strings.TrimSpace(selectExpr)
	if selectExpr == "" {
		selectExpr = "*"
	}

	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(selectExpr)
	sb.WriteString(" FROM ")
	sb.WriteString(d.QualifiedTable(namespace, table))
	sb.WriteString(normalizeClause(whereClause, "WHERE"))
	sb.WriteString(normalizeClause(orderByClause, "ORDER BY"))
	sb.WriteString(d.limitOffsetClause(limit, offset))
	return sb.String()
}

func (d Dialect) CountTableSQL(namespace, table, whereClause string) string {
	return "SELECT COUNT(*) AS total FROM " + d.QualifiedTable(namespace, table) + normalizeClause(whereClause, "WHERE")
}

func (d Dialect) SubqueryAlias(alias string) string {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return ""
	}
	if d.IsOracle() {
		return " " + alias
	}
	return " AS " + alias
}

func (d Dialect) AppendPaginationSQL(query string, limit, offset int) string {
	query = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(query), ";"))
	return query + d.limitOffsetClause(limit, offset)
}

func PaginateQuerySQL(query string, limit, offset int) string {
	return ForDialect("").PaginateQuerySQL(query, limit, offset)
}

func (d Dialect) PaginateQuerySQL(query string, limit, offset int) string {
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(query), ";"))
	return fmt.Sprintf("SELECT * FROM (%s)%s%s", inner, d.SubqueryAlias("addp_page"), d.limitOffsetClause(limit, offset))
}

func CountSubquerySQL(query, alias string) string {
	if strings.TrimSpace(alias) == "" {
		alias = "subquery"
	}
	return fmt.Sprintf("SELECT COUNT(*) AS total FROM (%s) AS %s", strings.TrimSpace(query), alias)
}

func SelectAllSampleSQL(dialectName, namespace, table string, limit int) string {
	dialect := ForDialect(dialectName)
	query := fmt.Sprintf("SELECT *\nFROM %s", dialect.QualifiedTable(namespace, table))
	query += strings.Replace(dialect.limitOffsetClause(limit, 0), " ", "\n", 1)
	return query
}

func normalizeClause(clause, keyword string) string {
	clause = strings.TrimSpace(clause)
	if clause == "" {
		return ""
	}
	upper := strings.ToUpper(clause)
	if strings.HasPrefix(upper, keyword+" ") {
		return " " + clause
	}
	return " " + keyword + " " + clause
}

func (d Dialect) limitOffsetClause(limit, offset int) string {
	if d.IsOracle() {
		switch {
		case limit > 0 && offset > 0:
			return fmt.Sprintf(" OFFSET %d ROWS FETCH NEXT %d ROWS ONLY", offset, limit)
		case limit > 0:
			return fmt.Sprintf(" FETCH FIRST %d ROWS ONLY", limit)
		case offset > 0:
			return fmt.Sprintf(" OFFSET %d ROWS", offset)
		default:
			return ""
		}
	}

	var sb strings.Builder
	if limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %d", limit))
	}
	if offset > 0 {
		sb.WriteString(fmt.Sprintf(" OFFSET %d", offset))
	}
	return sb.String()
}
