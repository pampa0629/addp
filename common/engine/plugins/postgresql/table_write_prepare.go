package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/sqldialect"
)

func (p *PostgreSQLPlugin) PrepareTableWrite(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.TableWriteOptions) error {
	mode := strings.ToLower(strings.TrimSpace(opts.Mode))
	switch mode {
	case "", "append", "insert":
		return nil
	case "truncate_insert", "create_if_not_exists":
	default:
		return fmt.Errorf("postgresql table write mode %q is not supported; supported modes: append, create_if_not_exists, truncate_insert", opts.Mode)
	}

	schema, table, err := tablePathParts(path)
	if err != nil {
		return err
	}
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build postgresql dsn: %w", err)
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to open postgresql connection: %w", err)
	}
	defer db.Close()

	switch mode {
	case "truncate_insert":
		if err := truncatePostgresTable(ctx, db, schema, table); err != nil {
			return err
		}
	case "create_if_not_exists":
		if err := createPostgresTableIfNotExists(ctx, db, schema, table, opts.Fields); err != nil {
			return err
		}
	}
	return nil
}

func truncatePostgresTable(ctx context.Context, db *sql.DB, schema, table string) error {
	truncateSQL := "TRUNCATE TABLE " + sqldialect.ForEngine("postgresql").QualifiedTable(schema, table)
	if _, err := db.ExecContext(ctx, truncateSQL); err != nil {
		return fmt.Errorf("truncate postgresql table %s.%s: %w", schema, table, err)
	}
	return nil
}

func createPostgresTableIfNotExists(ctx context.Context, db *sql.DB, schema, table string, fields []plugin.FieldInfo) error {
	if len(fields) == 0 {
		return fmt.Errorf("postgresql create_if_not_exists requires table fields")
	}
	dialect := sqldialect.ForEngine("postgresql")
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+dialect.QuoteIdentifier(schema)); err != nil {
		return fmt.Errorf("create postgresql schema %s: %w", schema, err)
	}

	definitions := make([]string, 0, len(fields))
	primaryKeys := make([]string, 0)
	seen := map[string]struct{}{}
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		definition := dialect.QuoteIdentifier(name) + " " + postgresSQLTypeForField(field)
		if !field.Nullable {
			definition += " NOT NULL"
		}
		definitions = append(definitions, definition)
		if field.PrimaryKey {
			primaryKeys = append(primaryKeys, dialect.QuoteIdentifier(name))
		}
	}
	if len(definitions) == 0 {
		return fmt.Errorf("postgresql create_if_not_exists requires at least one named field")
	}
	if len(primaryKeys) > 0 {
		definitions = append(definitions, "PRIMARY KEY ("+strings.Join(primaryKeys, ", ")+")")
	}

	createSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", dialect.QualifiedTable(schema, table), strings.Join(definitions, ", "))
	if _, err := db.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("create postgresql table %s.%s: %w", schema, table, err)
	}
	return nil
}

func postgresSQLTypeForField(field plugin.FieldInfo) string {
	if nativeType := strings.TrimSpace(field.NativeType); nativeType != "" {
		return nativeType
	}
	switch strings.ToLower(strings.TrimSpace(field.Type)) {
	case "string", "":
		return "TEXT"
	case "int":
		return "INTEGER"
	case "bigint":
		return "BIGINT"
	case "float":
		return "REAL"
	case "double":
		return "DOUBLE PRECISION"
	case "decimal":
		return "NUMERIC"
	case "bool", "boolean":
		return "BOOLEAN"
	case "date":
		return "DATE"
	case "time":
		return "TIME"
	case "timestamp", "datetime":
		return "TIMESTAMP"
	case "bytes", "binary":
		return "BYTEA"
	case "geometry":
		return "GEOMETRY"
	case "point":
		return "GEOMETRY(Point)"
	case "linestring":
		return "GEOMETRY(LineString)"
	case "polygon":
		return "GEOMETRY(Polygon)"
	case "multipoint":
		return "GEOMETRY(MultiPoint)"
	case "json":
		return "JSONB"
	case "uuid":
		return "UUID"
	case "array":
		return "TEXT[]"
	default:
		return "TEXT"
	}
}
