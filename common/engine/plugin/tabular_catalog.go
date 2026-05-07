package plugin

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

const (
	CatalogTermSchema   = "schema"
	CatalogTermDatabase = "database"
	CatalogTermTable    = "table"

	CatalogKindNamespace = "namespace"
	CatalogKindTable     = "table"
)

// TabularCatalogModel describes a tabular engine hierarchy.
func TabularCatalogModel(namespaceTerm string) CatalogModelSpec {
	if namespaceTerm == "" {
		namespaceTerm = CatalogTermDatabase
	}
	return CatalogModelSpec{
		PathVersion: CatalogPathVersion,
		RootTerm:    "server",
		Levels: []CatalogLevelSpec{
			{Term: namespaceTerm, Kinds: []string{CatalogKindNamespace}, Container: true},
			{Term: CatalogTermTable, Kinds: []string{CatalogKindTable, "view", "materialized_view", "external_table"}, Item: true},
		},
	}
}

type TabularMetadataAdapter struct {
	Plugin        RelationalDBPlugin
	NamespaceTerm string
	ListSchemas   func(ctx context.Context, db *gorm.DB) ([]SchemaInfo, error)
	ListTables    func(ctx context.Context, db *gorm.DB, schema string) ([]TableInfo, error)
	ListColumns   func(ctx context.Context, db *gorm.DB, schema, table string) ([]ColumnInfo, error)
	RowCount      func(ctx context.Context, db *gorm.DB, schema, table string) (int64, error)
}

// ListTabularCatalogChildren adapts plugin-local tabular metadata helpers to CatalogProvider.
func ListTabularCatalogChildren(ctx context.Context, adapter TabularMetadataAdapter, engine *Engine, parent CatalogPath, opts ListOptions) ([]CatalogNode, error) {
	if err := adapter.validate(); err != nil {
		return nil, err
	}
	db, err := GetOrCreatePoolFromFactory(engine, DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("获取连接池失败：%w", err)
	}

	namespaceTerm := adapter.namespaceTerm()
	if len(parent.Segments) == 0 {
		schemas, err := adapter.ListSchemas(ctx, db)
		if err != nil {
			return nil, err
		}
		nodes := make([]CatalogNode, 0, len(schemas))
		for _, schema := range schemas {
			if adapter.Plugin.IsSystemSchema(schema.Name) {
				continue
			}
			nodes = append(nodes, CatalogNode{
				Name:        schema.Name,
				Path:        appendCatalogSegment(parent, engine.ID, namespaceTerm, CatalogKindNamespace, schema.Name),
				Term:        namespaceTerm,
				Kind:        CatalogKindNamespace,
				IsContainer: true,
				Stats: map[string]interface{}{
					"table_count": schema.TableCount,
				},
			})
		}
		return nodes, nil
	}

	namespace := parent.Segments[0].Name
	tables, err := adapter.ListTables(ctx, db, namespace)
	if err != nil {
		return nil, err
	}
	nodes := make([]CatalogNode, 0, len(tables))
	for _, table := range tables {
		nodes = append(nodes, CatalogNode{
			Name:   table.TableName,
			Path:   appendCatalogSegment(parent, engine.ID, CatalogTermTable, CatalogKindTable, table.TableName),
			Term:   CatalogTermTable,
			Kind:   tableCatalogKind(table),
			IsItem: true,
			Stats: map[string]interface{}{
				"row_count":  table.RowCount,
				"size_bytes": table.SizeBytes,
			},
			Attributes: map[string]interface{}{
				"schema": table.Schema,
			},
		})
	}
	return nodes, nil
}

// ResolveTabularCatalogPath resolves a namespace or table node.
func ResolveTabularCatalogPath(ctx context.Context, adapter TabularMetadataAdapter, engine *Engine, path CatalogPath) (*CatalogNode, error) {
	if err := adapter.validate(); err != nil {
		return nil, err
	}
	if len(path.Segments) == 0 {
		return &CatalogNode{
			Name:        "",
			Path:        CatalogPath{Version: CatalogPathVersion, EngineID: engine.ID},
			Term:        "server",
			Kind:        "server",
			IsContainer: true,
		}, nil
	}

	last := path.Segments[len(path.Segments)-1]
	if len(path.Segments) == 1 {
		return &CatalogNode{
			Name:        last.Name,
			Path:        path,
			Term:        adapter.namespaceTerm(),
			Kind:        CatalogKindNamespace,
			IsContainer: true,
		}, nil
	}

	item, err := DescribeTabularItem(ctx, adapter, engine, path, MetadataOptions{})
	if err != nil {
		return nil, err
	}
	kind := item.Kind
	if kind == "" {
		kind = CatalogKindTable
	}
	return &CatalogNode{
		Name:   last.Name,
		Path:   path,
		Term:   CatalogTermTable,
		Kind:   kind,
		IsItem: true,
		Stats:  item.Stats,
	}, nil
}

// DescribeTabularItem adapts plugin-local column helpers and table stats to ItemMetadataProvider.
func DescribeTabularItem(ctx context.Context, adapter TabularMetadataAdapter, engine *Engine, path CatalogPath, opts MetadataOptions) (*ItemMetadata, error) {
	if err := adapter.validate(); err != nil {
		return nil, err
	}
	if len(path.Segments) < 2 {
		return nil, fmt.Errorf("tabular item path requires namespace and table segments")
	}

	db, err := GetOrCreatePoolFromFactory(engine, DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("获取连接池失败：%w", err)
	}

	namespace := path.Segments[0].Name
	table := path.Segments[len(path.Segments)-1].Name
	columns, err := adapter.ListColumns(ctx, db, namespace, table)
	if err != nil {
		return nil, err
	}

	fields := make([]FieldInfo, 0, len(columns))
	for _, col := range columns {
		fields = append(fields, FieldInfo{
			Name:       col.ColumnName,
			Type:       col.DataType,
			NativeType: col.DataType,
			Nullable:   col.IsNullable,
			PrimaryKey: col.IsPrimaryKey,
			Comment:    col.Comment,
		})
	}

	stats := map[string]interface{}{}
	if adapter.RowCount != nil {
		rowCount, err := adapter.RowCount(ctx, db, namespace, table)
		if err == nil {
			stats["row_count"] = rowCount
		}
	}

	return &ItemMetadata{
		Path:   path,
		Kind:   catalogKindFromTableName(ctx, adapter, db, namespace, table),
		Fields: fields,
		Stats:  stats,
		Attributes: map[string]interface{}{
			"namespace": namespace,
			"table":     table,
		},
	}, nil
}

func catalogKindFromTableName(ctx context.Context, adapter TabularMetadataAdapter, db *gorm.DB, namespace, tableName string) string {
	if adapter.ListTables == nil {
		return CatalogKindTable
	}
	tables, err := adapter.ListTables(ctx, db, namespace)
	if err != nil {
		return CatalogKindTable
	}
	for _, table := range tables {
		if table.TableName == tableName {
			return tableCatalogKind(table)
		}
	}
	return CatalogKindTable
}

func tableCatalogKind(table TableInfo) string {
	kind := table.Kind
	if kind == "" {
		kind = CatalogKindTable
	}
	return kind
}

func (a TabularMetadataAdapter) validate() error {
	if a.Plugin == nil {
		return fmt.Errorf("tabular metadata adapter plugin cannot be nil")
	}
	if a.ListSchemas == nil || a.ListTables == nil || a.ListColumns == nil {
		return fmt.Errorf("tabular metadata adapter is incomplete")
	}
	return nil
}

func (a TabularMetadataAdapter) namespaceTerm() string {
	if a.NamespaceTerm != "" {
		return a.NamespaceTerm
	}
	return CatalogTermDatabase
}
