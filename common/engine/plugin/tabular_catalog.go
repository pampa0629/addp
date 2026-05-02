package plugin

import (
	"context"
	"fmt"
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

// ListTabularCatalogChildren adapts RelationalDBPlugin metadata methods to CatalogProvider.
func ListTabularCatalogChildren(ctx context.Context, dbPlugin RelationalDBPlugin, engine *Engine, parent CatalogPath, opts ListOptions) ([]CatalogNode, error) {
	db, err := GetOrCreatePoolFromFactory(engine, DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("获取连接池失败：%w", err)
	}

	namespaceTerm := dbPlugin.SchemaNodeType()
	if len(parent.Segments) == 0 {
		schemas, err := dbPlugin.ListSchemas(ctx, db)
		if err != nil {
			return nil, err
		}
		nodes := make([]CatalogNode, 0, len(schemas))
		for _, schema := range schemas {
			if dbPlugin.IsSystemSchema(schema.Name) {
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
	tables, err := dbPlugin.ListTables(ctx, db, namespace)
	if err != nil {
		return nil, err
	}
	nodes := make([]CatalogNode, 0, len(tables))
	for _, table := range tables {
		nodes = append(nodes, CatalogNode{
			Name:   table.TableName,
			Path:   appendCatalogSegment(parent, engine.ID, CatalogTermTable, CatalogKindTable, table.TableName),
			Term:   CatalogTermTable,
			Kind:   CatalogKindTable,
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
func ResolveTabularCatalogPath(ctx context.Context, dbPlugin RelationalDBPlugin, engine *Engine, path CatalogPath) (*CatalogNode, error) {
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
			Term:        dbPlugin.SchemaNodeType(),
			Kind:        CatalogKindNamespace,
			IsContainer: true,
		}, nil
	}

	item, err := DescribeTabularItem(ctx, dbPlugin, engine, path, MetadataOptions{})
	if err != nil {
		return nil, err
	}
	return &CatalogNode{
		Name:   last.Name,
		Path:   path,
		Term:   CatalogTermTable,
		Kind:   CatalogKindTable,
		IsItem: true,
		Stats:  item.Stats,
	}, nil
}

// DescribeTabularItem adapts table columns and table stats to ItemMetadataProvider.
func DescribeTabularItem(ctx context.Context, dbPlugin RelationalDBPlugin, engine *Engine, path CatalogPath, opts MetadataOptions) (*ItemMetadata, error) {
	if len(path.Segments) < 2 {
		return nil, fmt.Errorf("tabular item path requires namespace and table segments")
	}

	db, err := GetOrCreatePoolFromFactory(engine, DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("获取连接池失败：%w", err)
	}

	namespace := path.Segments[0].Name
	table := path.Segments[len(path.Segments)-1].Name
	columns, err := dbPlugin.ListColumns(ctx, db, namespace, table)
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
	if rowCount, err := dbPlugin.GetTableRowCount(ctx, db, namespace, table); err == nil {
		stats["row_count"] = rowCount
	}

	return &ItemMetadata{
		Path:   path,
		Kind:   CatalogKindTable,
		Fields: fields,
		Stats:  stats,
		Attributes: map[string]interface{}{
			"namespace": namespace,
			"table":     table,
		},
	}, nil
}
