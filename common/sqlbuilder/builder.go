package sqlbuilder

import (
	"fmt"
	"strings"
)

// QuoteIdentifier 使用双引号包裹标识符以保留大小写
// PostgreSQL 中不带引号的标识符会自动转换为小写，使用双引号可以保留原始大小写
//
// 示例:
//   QuoteIdentifier("SmID") -> "SmID"
//   QuoteIdentifier("my\"column") -> "my""column"  (转义内部双引号)
func QuoteIdentifier(identifier string) string {
	// 转义标识符中的双引号（PostgreSQL 规范：双引号转义为两个双引号）
	escaped := strings.ReplaceAll(identifier, `"`, `""`)
	return `"` + escaped + `"`
}

// QuoteIdentifiers 批量引用多个标识符
func QuoteIdentifiers(identifiers []string) []string {
	result := make([]string, len(identifiers))
	for i, id := range identifiers {
		result[i] = QuoteIdentifier(id)
	}
	return result
}

// QualifiedTableName 生成安全的 "schema"."table" 格式
//
// 示例:
//   QualifiedTableName("public", "MyTable") -> "public"."MyTable"
//   QualifiedTableName("", "MyTable") -> "MyTable"
func QualifiedTableName(schema, table string) string {
	if schema == "" {
		return QuoteIdentifier(table)
	}
	return QuoteIdentifier(schema) + "." + QuoteIdentifier(table)
}

// GeometryTransform 生成安全的几何转换 SQL
//
// 示例:
//   GeometryTransform("SmGeometry", 3857) -> ST_Transform("SmGeometry", 3857)
func GeometryTransform(geomColumn string, targetSRID int) string {
	return fmt.Sprintf(`ST_Transform(%s, %d)`, QuoteIdentifier(geomColumn), targetSRID)
}

// SelectColumns 生成 SELECT 子句的列列表
//
// 示例:
//   SelectColumns([]string{"SmID", "Name", "SmGeometry"}) -> "SmID", "Name", "SmGeometry"
func SelectColumns(columns []string) string {
	return strings.Join(QuoteIdentifiers(columns), ", ")
}

// WhereClause 生成 WHERE 子句（如果条件为空则返回空字符串）
//
// 示例:
//   WhereClause([]string{"id > 10", "name IS NOT NULL"}) -> WHERE id > 10 AND name IS NOT NULL
//   WhereClause([]string{}) -> ""
func WhereClause(conditions []string) string {
	if len(conditions) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(conditions, " AND ")
}

// CreateTableSQL 生成 CREATE TABLE 语句
//
// 参数:
//   - schema: 模式名（可为空）
//   - table: 表名
//   - columnDefs: 列定义列表（例如：["id SERIAL PRIMARY KEY", "name VARCHAR(100)"]）
//   - ifNotExists: 是否添加 IF NOT EXISTS
//
// 示例:
//   CreateTableSQL("public", "MyTable", []string{"id SERIAL", "name TEXT"}, true)
//   -> CREATE TABLE IF NOT EXISTS "public"."MyTable" (id SERIAL, name TEXT)
func CreateTableSQL(schema, table string, columnDefs []string, ifNotExists bool) string {
	var sb strings.Builder
	sb.WriteString("CREATE TABLE ")
	if ifNotExists {
		sb.WriteString("IF NOT EXISTS ")
	}
	sb.WriteString(QualifiedTableName(schema, table))
	sb.WriteString(" (")
	sb.WriteString(strings.Join(columnDefs, ", "))
	sb.WriteString(")")
	return sb.String()
}

// CreateIndexSQL 生成 CREATE INDEX 语句
//
// 参数:
//   - indexName: 索引名
//   - schema: 模式名（可为空）
//   - table: 表名
//   - columns: 索引列列表
//   - indexType: 索引类型（例如：GIST, BTREE，可为空）
//   - concurrently: 是否使用 CONCURRENTLY
//
// 示例:
//   CreateIndexSQL("idx_geom", "public", "MyTable", []string{"SmGeometry"}, "GIST", true)
//   -> CREATE INDEX CONCURRENTLY "idx_geom" ON "public"."MyTable" USING GIST ("SmGeometry")
func CreateIndexSQL(indexName, schema, table string, columns []string, indexType string, concurrently bool) string {
	var sb strings.Builder
	sb.WriteString("CREATE INDEX ")
	if concurrently {
		sb.WriteString("CONCURRENTLY ")
	}
	sb.WriteString(QuoteIdentifier(indexName))
	sb.WriteString(" ON ")
	sb.WriteString(QualifiedTableName(schema, table))
	if indexType != "" {
		sb.WriteString(" USING ")
		sb.WriteString(indexType)
	}
	sb.WriteString(" (")
	sb.WriteString(SelectColumns(columns))
	sb.WriteString(")")
	return sb.String()
}

// AnalyzeTableSQL 生成 ANALYZE 语句
//
// 示例:
//   AnalyzeTableSQL("public", "MyTable") -> ANALYZE "public"."MyTable"
func AnalyzeTableSQL(schema, table string) string {
	return "ANALYZE " + QualifiedTableName(schema, table)
}

// DropTableSQL 生成 DROP TABLE 语句
//
// 示例:
//   DropTableSQL("public", "MyTable", true, true) -> DROP TABLE IF EXISTS "public"."MyTable" CASCADE
func DropTableSQL(schema, table string, ifExists, cascade bool) string {
	var sb strings.Builder
	sb.WriteString("DROP TABLE ")
	if ifExists {
		sb.WriteString("IF EXISTS ")
	}
	sb.WriteString(QualifiedTableName(schema, table))
	if cascade {
		sb.WriteString(" CASCADE")
	}
	return sb.String()
}

// InsertSQL 生成 INSERT 语句
//
// 参数:
//   - schema: 模式名（可为空）
//   - table: 表名
//   - columns: 列名列表
//   - placeholders: 占位符数量（生成 $1, $2, ... 或 ?, ?, ...）
//   - useNumberedPlaceholders: 是否使用编号占位符（PostgreSQL 使用 $1, MySQL 使用 ?）
//
// 示例:
//   InsertSQL("public", "MyTable", []string{"id", "name"}, 2, true)
//   -> INSERT INTO "public"."MyTable" ("id", "name") VALUES ($1, $2)
func InsertSQL(schema, table string, columns []string, placeholders int, useNumberedPlaceholders bool) string {
	var sb strings.Builder
	sb.WriteString("INSERT INTO ")
	sb.WriteString(QualifiedTableName(schema, table))
	sb.WriteString(" (")
	sb.WriteString(SelectColumns(columns))
	sb.WriteString(") VALUES (")

	for i := 0; i < placeholders; i++ {
		if i > 0 {
			sb.WriteString(", ")
		}
		if useNumberedPlaceholders {
			sb.WriteString(fmt.Sprintf("$%d", i+1))
		} else {
			sb.WriteString("?")
		}
	}
	sb.WriteString(")")
	return sb.String()
}

// SelectSQL 生成基础 SELECT 语句
//
// 参数:
//   - columns: 列名列表（空数组表示 SELECT *）
//   - schema: 模式名（可为空）
//   - table: 表名
//   - whereConditions: WHERE 条件列表（可为空）
//   - orderBy: ORDER BY 子句（可为空）
//   - limit: LIMIT 值（0 表示无限制）
//   - offset: OFFSET 值（0 表示无偏移）
//
// 示例:
//   SelectSQL([]string{"id", "name"}, "public", "MyTable", []string{"id > 10"}, "id ASC", 10, 0)
//   -> SELECT "id", "name" FROM "public"."MyTable" WHERE id > 10 ORDER BY id ASC LIMIT 10
func SelectSQL(columns []string, schema, table string, whereConditions []string, orderBy string, limit, offset int) string {
	var sb strings.Builder
	sb.WriteString("SELECT ")

	if len(columns) == 0 {
		sb.WriteString("*")
	} else {
		sb.WriteString(SelectColumns(columns))
	}

	sb.WriteString(" FROM ")
	sb.WriteString(QualifiedTableName(schema, table))

	if len(whereConditions) > 0 {
		sb.WriteString(WhereClause(whereConditions))
	}

	if orderBy != "" {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(orderBy)
	}

	if limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %d", limit))
	}

	if offset > 0 {
		sb.WriteString(fmt.Sprintf(" OFFSET %d", offset))
	}

	return sb.String()
}
