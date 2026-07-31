package shared

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/sqldialect"
	"gorm.io/gorm"
)

// MySQLCompatibleCatalogFactsDialect provides information_schema helpers for MySQL-compatible engines.
type MySQLCompatibleCatalogFactsDialect struct {
	SystemSchemas  map[string]bool
	IncludeComment bool
	IncludeEngine  bool
	MapFieldType   func(nativeType string) datatype.FieldType
}

func (d MySQLCompatibleCatalogFactsDialect) ListNamespaces(ctx context.Context, db *gorm.DB, root plugin.CatalogPath, namespaceTerm string) ([]plugin.CatalogEntry, error) {
	var rows []mysqlCompatibleNamespaceRow
	systemSchemas := d.systemSchemaNames()
	query := fmt.Sprintf(`
		SELECT
			schema_name as name,
			(SELECT COUNT(*)
			 FROM information_schema.tables
			 WHERE table_schema = s.schema_name
			   AND table_type = 'BASE TABLE') as leaf_count
		FROM information_schema.schemata s
		WHERE schema_name NOT IN (%s)
		ORDER BY schema_name
	`, sqlPlaceholders(len(systemSchemas)))

	args := make([]interface{}, 0, len(systemSchemas))
	for _, name := range systemSchemas {
		args = append(args, name)
	}

	if err := db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}
	namespaces := make([]plugin.CatalogEntry, 0, len(rows))
	for _, row := range rows {
		namespaces = append(namespaces, plugin.TabularNamespaceCatalogEntry(root, namespaceTerm, row.Name, row.LeafCount))
	}
	return namespaces, nil
}

type mysqlCompatibleNamespaceRow struct {
	Name      string
	LeafCount int
}

func (d MySQLCompatibleCatalogFactsDialect) ListTables(ctx context.Context, db *gorm.DB, schema string) ([]datatype.TableInfo, error) {
	var rows []mysqlCompatibleTableRow
	commentExpr := "'' as comment"
	if d.IncludeComment {
		commentExpr = "COALESCE(table_comment, '') as comment"
	}
	engineExpr := "'' as engine"
	if d.IncludeEngine {
		engineExpr = "COALESCE(engine, '') as engine"
	}
	query := `
		SELECT
			table_name as name,
			` + engineExpr + `,
			CASE
				WHEN table_type = 'VIEW' THEN 'view'
				WHEN table_type = 'BASE TABLE' THEN 'table'
				ELSE LOWER(REPLACE(table_type, ' ', '_'))
			END AS kind,
			` + commentExpr + `,
			table_rows as row_count,
			COALESCE(data_length + index_length, 0) as size_bytes
		FROM information_schema.tables
		WHERE table_schema = ?
		  AND table_type IN ('BASE TABLE', 'VIEW')
		ORDER BY table_name
	`

	if err := db.WithContext(ctx).Raw(query, schema).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}
	tables := make([]datatype.TableInfo, 0, len(rows))
	for _, row := range rows {
		tables = append(tables, datatype.TableInfo{
			Name:              row.Name,
			Kind:              row.Kind,
			Comment:           row.Comment,
			EstimatedRowCount: row.RowCount,
			SizeBytes:         row.SizeBytes,
			Native:            d.tableNative(row.Engine),
		})
	}
	return tables, nil
}

type mysqlCompatibleTableRow struct {
	Name      string
	Engine    string
	Kind      string
	Comment   string
	RowCount  *int64
	SizeBytes *int64
}

var mysqlCompatibleTableNativeKeys = datatype.NewNativeAllowedKeys("engine")

func (d MySQLCompatibleCatalogFactsDialect) tableNative(engine string) map[string]interface{} {
	if !d.IncludeEngine {
		return nil
	}
	engine = strings.TrimSpace(engine)
	if engine == "" {
		return nil
	}
	return datatype.FilterTableNative(map[string]interface{}{"engine": engine}, mysqlCompatibleTableNativeKeys)
}

func (d MySQLCompatibleCatalogFactsDialect) ListColumns(ctx context.Context, db *gorm.DB, schema, table string) ([]datatype.FieldInfo, error) {
	var fields []datatype.FieldInfo
	query := `
		SELECT
			column_name as name,
			column_type as native_type,
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
	for i := range fields {
		fields[i].Type = datatype.FieldTypeUnknown
		if d.MapFieldType != nil {
			fields[i].Type = d.MapFieldType(fields[i].NativeType)
		}
	}
	return plugin.NormalizeFieldInfos(fields), nil
}

func (d MySQLCompatibleCatalogFactsDialect) RowCount(ctx context.Context, db *gorm.DB, schema, table string) (int64, error) {
	var count int64
	query := sqldialect.ForEngine("mysql").CountTableSQL(schema, table, "")
	if err := db.WithContext(ctx).Raw(query).Scan(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to get row count: %w", err)
	}
	return count, nil
}

func (d MySQLCompatibleCatalogFactsDialect) IsSystemSchema(schemaName string) bool {
	return d.SystemSchemas[strings.ToLower(schemaName)]
}

func (d MySQLCompatibleCatalogFactsDialect) systemSchemaNames() []string {
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
