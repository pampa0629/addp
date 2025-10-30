package scanners

import (
	"database/sql"
	"fmt"

	"github.com/addp/common/format"
	_ "github.com/go-sql-driver/mysql"
)

// MySQLScanner 执行 MySQL 元数据扫描
type MySQLScanner struct {
	db *sql.DB
}

// NewMySQLScanner 根据连接串创建 MySQL 扫描器
func NewMySQLScanner(connStr string) (*MySQLScanner, error) {
	db, err := sql.Open("mysql", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to mysql: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping mysql: %w", err)
	}

	return &MySQLScanner{db: db}, nil
}

// ListSchemas 列出可用数据库
func (s *MySQLScanner) ListSchemas() ([]format.SchemaInfo, error) {
	query := `
		SELECT
			SCHEMA_NAME AS name,
			0 AS table_count,
			0 AS total_size
		FROM information_schema.SCHEMATA
		WHERE SCHEMA_NAME NOT IN ('information_schema', 'mysql', 'performance_schema', 'sys')
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

		tableCountQuery := `
			SELECT COUNT(*)
			FROM information_schema.TABLES
			WHERE TABLE_SCHEMA = ?
		`
		s.db.QueryRow(tableCountQuery, schema.Name).Scan(&schema.TableCount)

		sizeQuery := `
			SELECT IFNULL(SUM(data_length + index_length), 0)
			FROM information_schema.TABLES
			WHERE TABLE_SCHEMA = ?
		`
		s.db.QueryRow(sizeQuery, schema.Name).Scan(&schema.TotalSizeBytes)

		schemas = append(schemas, schema)
	}

	return schemas, nil
}

// ScanTables 扫描指定数据库的表
func (s *MySQLScanner) ScanTables(schemaName string) ([]format.TableInfo, error) {
	query := `
		SELECT
			TABLE_NAME AS table_name,
			TABLE_TYPE AS table_type,
			IFNULL(TABLE_COMMENT, '') AS table_comment,
			IFNULL(TABLE_ROWS, 0) AS row_count,
			IFNULL(DATA_LENGTH + INDEX_LENGTH, 0) AS size_bytes
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = ?
		ORDER BY TABLE_NAME
	`

	rows, err := s.db.Query(query, schemaName)
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
func (s *MySQLScanner) ScanFields(schemaName, tableName string) ([]format.FieldInfo, error) {
	query := `
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
		WHERE TABLE_SCHEMA = ?
		  AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION
	`

	rows, err := s.db.Query(query, schemaName, tableName)
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
func (s *MySQLScanner) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
