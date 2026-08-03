package sqldialect

import (
	"fmt"
	"strings"
)

const (
	doubleQuote = `"`
	backtick    = "`"
)

type Dialect struct {
	engineType string
	quote      string
}

func ForEngine(engineType string) Dialect {
	normalized := strings.ToLower(strings.TrimSpace(engineType))
	switch normalized {
	case "mysql", "doris", "clickhouse", "spark", "spark_sql":
		return Dialect{engineType: normalized, quote: backtick}
	default:
		return Dialect{engineType: normalized, quote: doubleQuote}
	}
}

func (d Dialect) EngineType() string {
	return d.engineType
}

func (d Dialect) IsPostgreSQL() bool {
	return d.engineType == "postgresql"
}

func (d Dialect) QuoteIdentifier(identifier string) string {
	quote := d.quote
	if quote == "" {
		quote = doubleQuote
	}
	return quote + strings.ReplaceAll(identifier, quote, quote+quote) + quote
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
	sb.WriteString(limitOffsetClause(limit, offset))
	return sb.String()
}

func (d Dialect) CountTableSQL(namespace, table, whereClause string) string {
	return "SELECT COUNT(*) AS total FROM " + d.QualifiedTable(namespace, table) + normalizeClause(whereClause, "WHERE")
}

func PaginateQuerySQL(query string, limit, offset int) string {
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(query), ";"))
	return fmt.Sprintf("SELECT * FROM (%s) AS addp_page%s", inner, limitOffsetClause(limit, offset))
}

func CountSubquerySQL(query, alias string) string {
	if strings.TrimSpace(alias) == "" {
		alias = "subquery"
	}
	return fmt.Sprintf("SELECT COUNT(*) AS total FROM (%s) AS %s", strings.TrimSpace(query), alias)
}

func SelectAllSampleSQL(engineType, namespace, table string, limit int) string {
	dialect := ForEngine(engineType)
	query := fmt.Sprintf("SELECT *\nFROM %s", dialect.QualifiedTable(namespace, table))
	if limit > 0 {
		query += fmt.Sprintf("\nLIMIT %d", limit)
	}
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

func limitOffsetClause(limit, offset int) string {
	var sb strings.Builder
	if limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %d", limit))
	}
	if offset > 0 {
		sb.WriteString(fmt.Sprintf(" OFFSET %d", offset))
	}
	return sb.String()
}
