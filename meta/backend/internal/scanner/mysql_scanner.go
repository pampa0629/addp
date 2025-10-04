package scanner

import (
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
)

type MySQLScanner struct {
	db *sql.DB
}

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

func (s *MySQLScanner) ScanDatabases() ([]DatabaseInfo, error) {
	query := `
		SELECT
			SCHEMA_NAME AS name,
			DEFAULT_CHARACTER_SET_NAME AS charset,
			DEFAULT_COLLATION_NAME AS collation,
			0 AS total_size
		FROM information_schema.SCHEMATA
		WHERE SCHEMA_NAME NOT IN ('information_schema', 'mysql', 'performance_schema', 'sys')
		ORDER BY SCHEMA_NAME
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query databases: %w", err)
	}
	defer rows.Close()

	var databases []DatabaseInfo
	for rows.Next() {
		var db DatabaseInfo
		err := rows.Scan(&db.Name, &db.Charset, &db.Collation, &db.TotalSizeBytes)
		if err != nil {
			continue
		}

		// 获取表数量
		tableCountQuery := `
			SELECT COUNT(*)
			FROM information_schema.TABLES
			WHERE TABLE_SCHEMA = ?
		`
		s.db.QueryRow(tableCountQuery, db.Name).Scan(&db.TableCount)

		// 获取数据库大小
		sizeQuery := `
			SELECT IFNULL(SUM(data_length + index_length), 0)
			FROM information_schema.TABLES
			WHERE TABLE_SCHEMA = ?
		`
		s.db.QueryRow(sizeQuery, db.Name).Scan(&db.TotalSizeBytes)

		databases = append(databases, db)
	}

	return databases, nil
}

func (s *MySQLScanner) ScanTables(database string) ([]TableInfo, error) {
	query := `
		SELECT
			TABLE_SCHEMA AS schema_name,
			TABLE_NAME AS table_name,
			TABLE_TYPE AS table_type,
			ENGINE AS engine,
			IFNULL(TABLE_ROWS, 0) AS row_count,
			IFNULL(DATA_LENGTH, 0) AS data_size,
			IFNULL(INDEX_LENGTH, 0) AS index_size,
			IFNULL(TABLE_COMMENT, '') AS table_comment
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = ?
		ORDER BY TABLE_NAME
	`

	rows, err := s.db.Query(query, database)
	if err != nil {
		return nil, fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var table TableInfo
		err := rows.Scan(
			&table.Schema,
			&table.Name,
			&table.Type,
			&table.Engine,
			&table.RowCount,
			&table.DataSize,
			&table.IndexSize,
			&table.Comment,
		)
		if err != nil {
			continue
		}

		tables = append(tables, table)
	}

	return tables, nil
}

func (s *MySQLScanner) ScanFields(database, table string) ([]FieldInfo, error) {
	query := `
		SELECT
			COLUMN_NAME AS field_name,
			ORDINAL_POSITION AS position,
			DATA_TYPE AS data_type,
			COLUMN_TYPE AS column_type,
			CASE WHEN IS_NULLABLE = 'YES' THEN true ELSE false END AS is_nullable,
			IFNULL(COLUMN_DEFAULT, '') AS column_default,
			IFNULL(COLUMN_KEY, '') AS column_key,
			IFNULL(EXTRA, '') AS extra,
			IFNULL(COLUMN_COMMENT, '') AS field_comment
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ?
		  AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION
	`

	rows, err := s.db.Query(query, database, table)
	if err != nil {
		return nil, fmt.Errorf("failed to query fields: %w", err)
	}
	defer rows.Close()

	var fields []FieldInfo
	for rows.Next() {
		var field FieldInfo
		err := rows.Scan(
			&field.Name,
			&field.Position,
			&field.DataType,
			&field.ColumnType,
			&field.IsNullable,
			&field.DefaultValue,
			&field.ColumnKey,
			&field.Extra,
			&field.Comment,
		)
		if err != nil {
			continue
		}

		fields = append(fields, field)
	}

	return fields, nil
}

func (s *MySQLScanner) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
