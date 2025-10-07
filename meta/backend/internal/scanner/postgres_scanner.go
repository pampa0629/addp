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

func (s *PostgresScanner) ListSchemas() ([]SchemaInfo, error) {
	query := `
		SELECT
			schema_name,
			(SELECT COUNT(*) FROM information_schema.tables t WHERE t.table_schema = schema_name) AS table_count
		FROM information_schema.schemata
		WHERE schema_name NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		ORDER BY schema_name
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query schemas: %w", err)
	}
	defer rows.Close()

	var schemas []SchemaInfo
	for rows.Next() {
		var schema SchemaInfo
		if err := rows.Scan(&schema.Name, &schema.TableCount); err != nil {
			return nil, err
		}
		schemas = append(schemas, schema)
	}

	return schemas, rows.Err()
}

func (s *PostgresScanner) ScanTables(schemaName string) ([]TableInfo, error) {
	query := `
		SELECT
			t.table_name,
			t.table_type,
			COALESCE(pg_catalog.obj_description(pgc.oid, 'pg_class'), '') AS table_comment,
			COALESCE(pgc.reltuples::bigint, 0) AS row_count,
			COALESCE(pg_total_relation_size(pgc.oid), 0) AS size_bytes
		FROM information_schema.tables t
		LEFT JOIN pg_catalog.pg_namespace pgn ON pgn.nspname = t.table_schema
		LEFT JOIN pg_catalog.pg_class pgc ON pgc.relname = t.table_name AND pgc.relnamespace = pgn.oid
		WHERE t.table_schema = $1
		ORDER BY t.table_name
	`

	rows, err := s.db.Query(query, schemaName)
	if err != nil {
		return nil, fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var table TableInfo
		if err := rows.Scan(&table.Name, &table.Type, &table.Comment, &table.RowCount, &table.SizeBytes); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}

	return tables, rows.Err()
}

func (s *PostgresScanner) ScanFields(schemaName, tableName string) ([]FieldInfo, error) {
	query := `
		SELECT
			c.column_name,
			c.ordinal_position,
			c.data_type,
			c.udt_name,
			CASE WHEN c.is_nullable = 'YES' THEN true ELSE false END,
			COALESCE(c.column_default, ''),
			COALESCE(pg_catalog.col_description(pgc.oid, c.ordinal_position::int), ''),
			CASE WHEN pk.column_name IS NOT NULL THEN true ELSE false END,
			CASE WHEN uq.column_name IS NOT NULL THEN true ELSE false END,
			COALESCE(c.character_set_name, ''),
			COALESCE(c.collation_name, ''),
			COALESCE(c.numeric_precision, 0),
			COALESCE(c.numeric_scale, 0)
		FROM information_schema.columns c
		LEFT JOIN pg_catalog.pg_namespace pgn ON pgn.nspname = c.table_schema
		LEFT JOIN pg_catalog.pg_class pgc ON pgc.relname = c.table_name AND pgc.relnamespace = pgn.oid
		LEFT JOIN (
			SELECT ku.column_name
			FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage ku ON tc.constraint_name = ku.constraint_name
			WHERE tc.table_schema = $1 AND tc.table_name = $2 AND tc.constraint_type = 'PRIMARY KEY'
		) pk ON pk.column_name = c.column_name
		LEFT JOIN (
			SELECT ku.column_name
			FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage ku ON tc.constraint_name = ku.constraint_name
			WHERE tc.table_schema = $1 AND tc.table_name = $2 AND tc.constraint_type = 'UNIQUE'
		) uq ON uq.column_name = c.column_name
		WHERE c.table_schema = $1 AND c.table_name = $2
		ORDER BY c.ordinal_position
	`

	rows, err := s.db.Query(query, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query fields: %w", err)
	}
	defer rows.Close()

	var fields []FieldInfo
	for rows.Next() {
		var field FieldInfo
		var udtName sql.NullString
		if err := rows.Scan(
			&field.Name,
			&field.OrdinalPosition,
			&field.DataType,
			&udtName,
			&field.IsNullable,
			&field.DefaultValue,
			&field.Comment,
			&field.IsPrimaryKey,
			&field.IsUniqueKey,
			&field.CharacterSet,
			&field.Collation,
			&field.NumericPrecision,
			&field.NumericScale,
		); err != nil {
			return nil, err
		}
		// 使用 udt_name 作为 ColumnType
		if udtName.Valid {
			field.ColumnType = udtName.String
		} else {
			field.ColumnType = field.DataType
		}
		fields = append(fields, field)
	}

	return fields, rows.Err()
}

func (s *PostgresScanner) Close() error {
	return s.db.Close()
}
