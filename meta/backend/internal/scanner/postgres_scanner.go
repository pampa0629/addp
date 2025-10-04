package scanner

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

type PostgresScanner struct {
	db *sql.DB
}

func NewPostgresScanner(connStr string) (*PostgresScanner, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	return &PostgresScanner{db: db}, nil
}

func (s *PostgresScanner) ScanDatabases() ([]DatabaseInfo, error) {
	query := `
		SELECT
			datname,
			pg_encoding_to_char(encoding) AS charset,
			datcollate AS collation,
			pg_database_size(datname) AS total_size
		FROM pg_database
		WHERE datistemplate = false
		  AND datname NOT IN ('postgres', 'template0', 'template1')
		ORDER BY datname
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
			FROM information_schema.tables
			WHERE table_catalog = $1
			  AND table_schema NOT IN ('pg_catalog', 'information_schema')
		`
		s.db.QueryRow(tableCountQuery, db.Name).Scan(&db.TableCount)

		databases = append(databases, db)
	}

	return databases, nil
}

func (s *PostgresScanner) ScanTables(database string) ([]TableInfo, error) {
	query := `
		SELECT
			t.table_schema,
			t.table_name,
			t.table_type,
			COALESCE(pg_stat_user_tables.n_live_tup, 0) AS row_count,
			COALESCE(pg_total_relation_size((t.table_schema||'.'||t.table_name)::regclass), 0) AS total_size,
			COALESCE(pg_relation_size((t.table_schema||'.'||t.table_name)::regclass), 0) AS data_size,
			COALESCE(pg_total_relation_size((t.table_schema||'.'||t.table_name)::regclass) -
			         pg_relation_size((t.table_schema||'.'||t.table_name)::regclass), 0) AS index_size,
			COALESCE(obj_description((t.table_schema||'.'||t.table_name)::regclass), '') AS table_comment
		FROM information_schema.tables t
		LEFT JOIN pg_stat_user_tables
			ON pg_stat_user_tables.schemaname = t.table_schema
			AND pg_stat_user_tables.relname = t.table_name
		WHERE t.table_catalog = $1
		  AND t.table_schema NOT IN ('pg_catalog', 'information_schema')
		ORDER BY t.table_schema, t.table_name
	`

	rows, err := s.db.Query(query, database)
	if err != nil {
		return nil, fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var table TableInfo
		var totalSize, dataSize, indexSize int64

		err := rows.Scan(
			&table.Schema,
			&table.Name,
			&table.Type,
			&table.RowCount,
			&totalSize,
			&dataSize,
			&indexSize,
			&table.Comment,
		)
		if err != nil {
			continue
		}

		table.DataSize = dataSize
		table.IndexSize = indexSize
		tables = append(tables, table)
	}

	return tables, nil
}

func (s *PostgresScanner) ScanFields(database, table string) ([]FieldInfo, error) {
	// 分离 schema 和 table name
	schemaName := "public"
	tableName := table

	query := `
		SELECT
			c.column_name,
			c.ordinal_position,
			c.data_type,
			c.udt_name AS column_type,
			CASE WHEN c.is_nullable = 'YES' THEN true ELSE false END AS is_nullable,
			COALESCE(c.column_default, '') AS column_default,
			COALESCE(
				(SELECT 'PRI' FROM information_schema.table_constraints tc
				 JOIN information_schema.key_column_usage kcu
				   ON tc.constraint_name = kcu.constraint_name
				   AND tc.table_schema = kcu.table_schema
				 WHERE tc.table_schema = c.table_schema
				   AND tc.table_name = c.table_name
				   AND kcu.column_name = c.column_name
				   AND tc.constraint_type = 'PRIMARY KEY'
				 LIMIT 1),
				''
			) AS column_key,
			'' AS extra,
			COALESCE(col_description((c.table_schema||'.'||c.table_name)::regclass, c.ordinal_position), '') AS field_comment
		FROM information_schema.columns c
		WHERE c.table_catalog = $1
		  AND c.table_schema = $2
		  AND c.table_name = $3
		ORDER BY c.ordinal_position
	`

	rows, err := s.db.Query(query, database, schemaName, tableName)
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

func (s *PostgresScanner) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
