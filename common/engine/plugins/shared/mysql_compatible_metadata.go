package shared

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"gorm.io/gorm"
)

// MySQLCompatibleMetadataDialect provides information_schema helpers for MySQL-compatible engines.
type MySQLCompatibleMetadataDialect struct {
	SystemSchemas  map[string]bool
	IncludeComment bool
}

func (d MySQLCompatibleMetadataDialect) ListSchemas(ctx context.Context, db *gorm.DB) ([]plugin.SchemaInfo, error) {
	var schemas []plugin.SchemaInfo
	systemSchemas := d.systemSchemaNames()
	query := fmt.Sprintf(`
		SELECT
			schema_name as name,
			(SELECT COUNT(*)
			 FROM information_schema.tables
			 WHERE table_schema = s.schema_name
			   AND table_type = 'BASE TABLE') as table_count
		FROM information_schema.schemata s
		WHERE schema_name NOT IN (%s)
		ORDER BY schema_name
	`, sqlPlaceholders(len(systemSchemas)))

	args := make([]interface{}, 0, len(systemSchemas))
	for _, name := range systemSchemas {
		args = append(args, name)
	}

	if err := db.WithContext(ctx).Raw(query, args...).Scan(&schemas).Error; err != nil {
		return nil, fmt.Errorf("failed to list schemas: %w", err)
	}
	return schemas, nil
}

func (d MySQLCompatibleMetadataDialect) ListTables(ctx context.Context, db *gorm.DB, schema string) ([]plugin.TableInfo, error) {
	var tables []plugin.TableInfo
	commentExpr := "'' as comment"
	if d.IncludeComment {
		commentExpr = "COALESCE(table_comment, '') as comment"
	}
	query := `
		SELECT
			table_schema as ` + "`schema`" + `,
			table_name as table_name,
			CASE
				WHEN table_type = 'VIEW' THEN 'view'
				WHEN table_type = 'BASE TABLE' THEN 'table'
				ELSE LOWER(REPLACE(table_type, ' ', '_'))
			END AS table_kind,
			` + commentExpr + `,
			COALESCE(table_rows, 0) as row_count,
			COALESCE(data_length + index_length, 0) as size_bytes
		FROM information_schema.tables
		WHERE table_schema = ?
		  AND table_type IN ('BASE TABLE', 'VIEW')
		ORDER BY table_name
	`

	if err := db.WithContext(ctx).Raw(query, schema).Scan(&tables).Error; err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}
	return tables, nil
}

func (d MySQLCompatibleMetadataDialect) ListColumns(ctx context.Context, db *gorm.DB, schema, table string) ([]datatype.FieldInfo, error) {
	var fields []datatype.FieldInfo
	query := `
		SELECT
			column_name as name,
			data_type as native_type,
			IF(is_nullable = 'YES', true, false) as nullable,
			IF(column_key = 'PRI', true, false) as primary_key,
			COALESCE(column_comment, '') as comment
		FROM information_schema.columns
		WHERE table_schema = ?
		  AND table_name = ?
		ORDER BY ordinal_position
	`

	if err := db.WithContext(ctx).Raw(query, schema, table).Scan(&fields).Error; err != nil {
		return nil, fmt.Errorf("failed to list columns: %w", err)
	}
	return plugin.NormalizeFieldInfos(fields), nil
}

func (d MySQLCompatibleMetadataDialect) RowCount(ctx context.Context, db *gorm.DB, schema, table string) (int64, error) {
	var count int64
	query := `
		SELECT COALESCE(table_rows, 0)
		FROM information_schema.tables
		WHERE table_schema = ?
		  AND table_name = ?
	`
	if err := db.WithContext(ctx).Raw(query, schema, table).Scan(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to get row count: %w", err)
	}
	return count, nil
}

func (d MySQLCompatibleMetadataDialect) IsSystemSchema(schemaName string) bool {
	return d.SystemSchemas[strings.ToLower(schemaName)]
}

func (d MySQLCompatibleMetadataDialect) systemSchemaNames() []string {
	names := make([]string, 0, len(d.SystemSchemas))
	for name := range d.SystemSchemas {
		names = append(names, name)
	}
	if len(names) == 0 {
		return []string{"information_schema"}
	}
	sort.Strings(names)
	return names
}

func sqlPlaceholders(n int) string {
	if n <= 0 {
		return "?"
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}
