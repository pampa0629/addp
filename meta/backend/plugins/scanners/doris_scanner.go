package scanners

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/format"
	_ "github.com/go-sql-driver/mysql"
)

// DorisScanner 执行 Doris 元数据扫描
// Doris 不完全支持 MySQL 的 prepared statements，需要使用普通查询
type DorisScanner struct {
	db *sql.DB
}

// NewDorisScanner 根据连接串创建 Doris 扫描器
func NewDorisScanner(connStr string) (*DorisScanner, error) {
	// Doris 不支持 prepared statements，需要在连接字符串中禁用
	// interpolateParams=true 会让驱动在客户端进行参数插值，而不是使用服务端的 prepared statement
	if !strings.Contains(connStr, "interpolateParams") {
		if strings.Contains(connStr, "?") {
			connStr += "&interpolateParams=true"
		} else {
			connStr += "?interpolateParams=true"
		}
	}

	db, err := sql.Open("mysql", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to doris: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping doris: %w", err)
	}

	return &DorisScanner{db: db}, nil
}

// ListSchemas 列出可用数据库
func (s *DorisScanner) ListSchemas() ([]format.SchemaInfo, error) {
	query := `
		SELECT
			SCHEMA_NAME AS name,
			0 AS table_count,
			0 AS total_size
		FROM information_schema.SCHEMATA
		WHERE SCHEMA_NAME NOT IN ('information_schema', 'mysql', 'performance_schema', 'sys', '__internal_schema')
		ORDER BY SCHEMA_NAME
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query schemas: %w", err)
	}
	defer rows.Close()

	var schemas []format.SchemaInfo
	for rows.Next() {
		var schema format.SchemaInfo
		if err := rows.Scan(&schema.Name, &schema.TableCount, &schema.TotalSizeBytes); err != nil {
			continue
		}

		// 获取表数量（使用字符串拼接而不是 prepared statement）
		tableCountQuery := fmt.Sprintf(`
			SELECT COUNT(*)
			FROM information_schema.TABLES
			WHERE TABLE_SCHEMA = '%s'
		`, schema.Name)
		s.db.QueryRow(tableCountQuery).Scan(&schema.TableCount)

		schemas = append(schemas, schema)
	}

	return schemas, nil
}

// ScanTables 扫描指定数据库的表
// 注意：Doris 中 Database 就是 Schema，需要切换到指定数据库
func (s *DorisScanner) ScanTables(schemaName string) ([]format.TableInfo, error) {
	// 切换到指定数据库
	if _, err := s.db.Exec(fmt.Sprintf("USE `%s`", schemaName)); err != nil {
		return nil, fmt.Errorf("failed to switch to database %s: %w", schemaName, err)
	}

	// 查询当前数据库的表
	query := fmt.Sprintf(`
		SELECT
			TABLE_NAME AS table_name,
			TABLE_TYPE AS table_type,
			IFNULL(TABLE_COMMENT, '') AS table_comment,
			0 AS row_count,
			0 AS size_bytes
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = '%s'
		ORDER BY TABLE_NAME
	`, schemaName)

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	var tables []format.TableInfo
	for rows.Next() {
		var table format.TableInfo
		if err := rows.Scan(
			&table.Name,
			&table.Type,
			&table.Comment,
			&table.RowCount,
			&table.SizeBytes,
		); err != nil {
			continue
		}
		tables = append(tables, table)
	}

	return tables, nil
}

// ScanFields 扫描指定表的字段
// 注意：Doris 中 Database 就是 Schema，需要切换到指定数据库
func (s *DorisScanner) ScanFields(schemaName, tableName string) ([]format.FieldInfo, error) {
	// 切换到指定数据库
	if _, err := s.db.Exec(fmt.Sprintf("USE `%s`", schemaName)); err != nil {
		return nil, fmt.Errorf("failed to switch to database %s: %w", schemaName, err)
	}

	// 查询字段信息
	query := fmt.Sprintf(`
		SELECT
			COLUMN_NAME AS field_name,
			ORDINAL_POSITION AS position,
			DATA_TYPE AS data_type,
			COLUMN_TYPE AS column_type,
			CASE WHEN IS_NULLABLE = 'YES' THEN 1 ELSE 0 END AS is_nullable,
			IFNULL(COLUMN_DEFAULT, '') AS column_default,
			IFNULL(COLUMN_COMMENT, '') AS field_comment,
			CASE WHEN COLUMN_KEY = 'PRI' THEN 1 ELSE 0 END AS is_primary_key,
			CASE WHEN COLUMN_KEY = 'UNI' THEN 1 ELSE 0 END AS is_unique_key,
			IFNULL(CHARACTER_SET_NAME, '') AS character_set,
			IFNULL(COLLATION_NAME, '') AS collation,
			IFNULL(NUMERIC_PRECISION, 0) AS numeric_precision,
			IFNULL(NUMERIC_SCALE, 0) AS numeric_scale
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = '%s'
		  AND TABLE_NAME = '%s'
		ORDER BY ORDINAL_POSITION
	`, schemaName, tableName)

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query fields: %w", err)
	}
	defer rows.Close()

	var fields []format.FieldInfo
	for rows.Next() {
		var field format.FieldInfo
		var isNullable, isPrimaryKey, isUniqueKey int
		if err := rows.Scan(
			&field.Name,
			&field.OrdinalPosition,
			&field.DataType,
			&field.ColumnType,
			&isNullable,
			&field.DefaultValue,
			&field.Comment,
			&isPrimaryKey,
			&isUniqueKey,
			&field.CharacterSet,
			&field.Collation,
			&field.NumericPrecision,
			&field.NumericScale,
		); err != nil {
			continue
		}

		field.IsNullable = isNullable == 1
		field.IsPrimaryKey = isPrimaryKey == 1
		field.IsUniqueKey = isUniqueKey == 1

		fields = append(fields, field)
	}

	return fields, nil
}

// Close 关闭数据库连接
func (s *DorisScanner) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
