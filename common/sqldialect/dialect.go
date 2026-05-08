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
	return strings.TrimSpace(query) + limitOffsetClause(limit, offset)
}

func CountSubquerySQL(query, alias string) string {
	if strings.TrimSpace(alias) == "" {
		alias = "subquery"
	}
	return fmt.Sprintf("SELECT COUNT(*) AS total FROM (%s) AS %s", strings.TrimSpace(query), alias)
}

func SelectAllSampleSQL(engineType, namespace, table string, limit int) string {
	if limit <= 0 {
		limit = 10
	}
	dialect := ForEngine(engineType)
	return fmt.Sprintf("SELECT *\nFROM %s\nLIMIT %d", dialect.QualifiedTable(namespace, table), limit)
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

func IsPostGISSpatialType(dataType string) bool {
	dataTypeLower := strings.ToLower(strings.TrimSpace(dataType))
	return dataTypeLower == "geometry" ||
		dataTypeLower == "geography" ||
		strings.HasPrefix(dataTypeLower, "geometry(") ||
		strings.HasPrefix(dataTypeLower, "geography(") ||
		dataTypeLower == "user-defined"
}

func IsPostGISGeographyType(dataType string) bool {
	dataTypeLower := strings.ToLower(strings.TrimSpace(dataType))
	return dataTypeLower == "geography" || strings.HasPrefix(dataTypeLower, "geography(")
}

func PostGISWKTExpression(columnName, dataType string) string {
	quotedColumn := ForEngine("postgresql").QuoteIdentifier(columnName)
	if IsPostGISGeographyType(dataType) {
		return fmt.Sprintf("ST_AsText(%s::geometry)", quotedColumn)
	}
	return fmt.Sprintf("ST_AsText(%s)", quotedColumn)
}

func PostGISRenderGeoJSONExpression(columnName, dataType string) string {
	quotedColumn := ForEngine("postgresql").QuoteIdentifier(columnName)
	if IsPostGISGeographyType(dataType) {
		return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE ST_AsGeoJSON(%s::geometry) END", quotedColumn, quotedColumn)
	}
	return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE ST_AsGeoJSON(ST_Transform(%s, 4326)) END", quotedColumn, quotedColumn)
}

func PostGISGeoJSONSelectExpression(columnName string) string {
	quotedColumn := ForEngine("postgresql").QuoteIdentifier(columnName)
	return fmt.Sprintf("ST_AsGeoJSON(%s) AS %s", quotedColumn, quotedColumn)
}
