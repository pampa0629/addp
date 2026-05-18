package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/common/sqldialect"
)

func (p *PostgreSQLPlugin) PrepareTableWrite(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.TableWriteOptions) error {
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

	return createPostgresTableIfNotExists(ctx, db, schema, table, opts.Fields)
}

func createPostgresTableIfNotExists(ctx context.Context, db *sql.DB, schema, table string, fields []plugin.FieldInfo) error {
	if len(fields) == 0 {
		return fmt.Errorf("postgresql table write prepare requires table fields")
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
		return fmt.Errorf("postgresql table write prepare requires at least one named field")
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

func (p *PostgreSQLPlugin) DeleteResource(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) error {
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

	dropSQL := "DROP TABLE IF EXISTS " + sqldialect.ForEngine("postgresql").QualifiedTable(schema, table)
	if _, err := db.ExecContext(ctx, dropSQL); err != nil {
		return fmt.Errorf("drop postgresql table %s.%s: %w", schema, table, err)
	}
	return nil
}

func postgresSQLTypeForField(field plugin.FieldInfo) string {
	if sqlType := postgresSpatialTypeForField(field); sqlType != "" {
		return sqlType
	}
	if sqlType, ok := postgresSQLTypeForCommonType(field.Type); ok {
		return sqlType
	}
	return "TEXT"
}

func postgresSpatialTypeForField(field plugin.FieldInfo) string {
	fieldType := strings.ToLower(strings.TrimSpace(field.Type))
	if fieldType != "geometry" && fieldType != "point" && fieldType != "linestring" && fieldType != "polygon" && fieldType != "multipoint" {
		return ""
	}
	geometryType := commonJSON.InterfaceString(field.Attributes["geometry_type"])
	if geometryType == "" {
		switch fieldType {
		case "point":
			geometryType = "Point"
		case "linestring":
			geometryType = "LineString"
		case "polygon":
			geometryType = "Polygon"
		case "multipoint":
			geometryType = "MultiPoint"
		}
	}
	if geometryType == "" {
		return ""
	}
	srid := commonJSON.InterfaceInt64(field.Attributes["srid"])
	if srid > 0 {
		return fmt.Sprintf("GEOMETRY(%s,%d)", geometryType, srid)
	}
	return fmt.Sprintf("GEOMETRY(%s)", geometryType)
}

func postgresSQLTypeForCommonType(fieldType string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(fieldType)) {
	case "string", "":
		return "TEXT", true
	case "int":
		return "INTEGER", true
	case "bigint":
		return "BIGINT", true
	case "float":
		return "REAL", true
	case "double":
		return "DOUBLE PRECISION", true
	case "decimal":
		return "NUMERIC", true
	case "bool", "boolean":
		return "BOOLEAN", true
	case "date":
		return "DATE", true
	case "time":
		return "TIME", true
	case "timestamp", "datetime":
		return "TIMESTAMP", true
	case "bytes", "binary":
		return "BYTEA", true
	case "geometry":
		return "GEOMETRY", true
	case "point":
		return "GEOMETRY(Point)", true
	case "linestring":
		return "GEOMETRY(LineString)", true
	case "polygon":
		return "GEOMETRY(Polygon)", true
	case "multipoint":
		return "GEOMETRY(MultiPoint)", true
	case "json":
		return "JSONB", true
	case "uuid":
		return "UUID", true
	case "array":
		return "TEXT[]", true
	default:
		return "", false
	}
}
