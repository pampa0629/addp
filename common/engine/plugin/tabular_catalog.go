package plugin

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/common/datatype"
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
			{Term: namespaceTerm, Kinds: []string{CatalogKindNamespace}, Container: true, I18nKey: CatalogTermI18nKey(namespaceTerm)},
			{Term: CatalogTermTable, Kinds: []string{CatalogKindTable, "view", "materialized_view", "external_table"}, Item: true, I18nKey: CatalogTermI18nKey(CatalogTermTable)},
		},
	}
}

type TabularCatalogCallbacks struct {
	NamespaceTerm         string
	ListNamespaces        func(ctx context.Context, db *gorm.DB) ([]NamespaceInfo, error)
	ListTables            func(ctx context.Context, db *gorm.DB, namespace string) ([]datatype.TableInfo, error)
	ListColumns           func(ctx context.Context, db *gorm.DB, namespace, table string) ([]datatype.FieldInfo, error)
	RowCount              func(ctx context.Context, db *gorm.DB, namespace, table string) (int64, error)
	IsSystemNamespaceFunc func(namespace string) bool
}

// ListTabularCatalogChildren maps tabular callbacks to CatalogProvider.
func ListTabularCatalogChildren(ctx context.Context, callbacks TabularCatalogCallbacks, engine *Engine, parent CatalogPath, opts ListOptions) ([]CatalogNode, error) {
	if err := callbacks.validate(); err != nil {
		return nil, err
	}
	db, err := GetOrCreatePoolFromFactory(engine, DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("获取连接池失败：%w", err)
	}

	namespaceTerm := callbacks.namespaceTerm()
	if len(parent.Segments) == 0 {
		namespaces, err := callbacks.ListNamespaces(ctx, db)
		if err != nil {
			return nil, err
		}
		nodes := make([]CatalogNode, 0, len(namespaces))
		for _, namespace := range namespaces {
			if callbacks.isSystemNamespace(namespace.Name) {
				continue
			}
			nodes = append(nodes, CatalogNode{
				Name:        namespace.Name,
				Path:        appendCatalogSegment(parent, engine.ID, namespaceTerm, CatalogKindNamespace, namespace.Name),
				Term:        namespaceTerm,
				Kind:        CatalogKindNamespace,
				IsContainer: true,
				Stats: map[string]interface{}{
					"table_count": namespace.TableCount,
				},
			})
		}
		return nodes, nil
	}

	namespace := parent.Segments[0].Name
	tables, err := callbacks.ListTables(ctx, db, namespace)
	if err != nil {
		return nil, err
	}
	nodes := make([]CatalogNode, 0, len(tables))
	for _, table := range tables {
		nodes = append(nodes, CatalogNode{
			Name:       table.Name,
			Path:       appendCatalogSegment(parent, engine.ID, CatalogTermTable, CatalogKindTable, table.Name),
			Term:       CatalogTermTable,
			Kind:       tableCatalogKind(table),
			IsItem:     true,
			Stats:      tableStats(table),
			Attributes: tableAttributes(namespace, table),
		})
	}
	return nodes, nil
}

// ResolveTabularCatalogPath resolves a namespace or table node.
func ResolveTabularCatalogPath(ctx context.Context, callbacks TabularCatalogCallbacks, engine *Engine, path CatalogPath) (*CatalogNode, error) {
	if err := callbacks.validate(); err != nil {
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
			Term:        callbacks.namespaceTerm(),
			Kind:        CatalogKindNamespace,
			IsContainer: true,
		}, nil
	}

	item, err := DescribeTabularItem(ctx, callbacks, engine, path, MetadataOptions{})
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

// DescribeTabularItem maps tabular column callbacks and table stats to ItemMetadataProvider.
func DescribeTabularItem(ctx context.Context, callbacks TabularCatalogCallbacks, engine *Engine, path CatalogPath, opts MetadataOptions) (*ItemMetadata, error) {
	if err := callbacks.validate(); err != nil {
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
	columns, err := callbacks.ListColumns(ctx, db, namespace, table)
	if err != nil {
		return nil, err
	}

	fields := NormalizeFieldInfos(columns)

	tableInfo, hasTableInfo := findTableInfo(ctx, callbacks, db, namespace, table)
	stats := map[string]interface{}{}
	kind := CatalogKindTable
	attrs := map[string]interface{}{
		"namespace": namespace,
		"table":     table,
	}
	var updatedAt *time.Time
	if hasTableInfo {
		kind = tableCatalogKind(tableInfo)
		stats = tableStats(tableInfo)
		attrs = tableAttributes(namespace, tableInfo)
		if _, ok := attrs["table"]; !ok {
			attrs["table"] = table
		}
		updatedAt = tableInfo.UpdatedAt
	}
	if callbacks.RowCount != nil {
		rowCount, err := callbacks.RowCount(ctx, db, namespace, table)
		if err == nil {
			stats["row_count"] = rowCount
		}
	}

	return &ItemMetadata{
		Path:       path,
		Kind:       kind,
		Fields:     fields,
		Stats:      stats,
		Attributes: attrs,
		UpdatedAt:  updatedAt,
	}, nil
}

func findTableInfo(ctx context.Context, callbacks TabularCatalogCallbacks, db *gorm.DB, namespace, tableName string) (datatype.TableInfo, bool) {
	if callbacks.ListTables == nil {
		return datatype.TableInfo{}, false
	}
	tables, err := callbacks.ListTables(ctx, db, namespace)
	if err != nil {
		return datatype.TableInfo{}, false
	}
	for _, table := range tables {
		if table.Name == tableName {
			return table, true
		}
	}
	return datatype.TableInfo{}, false
}

func tableCatalogKind(table datatype.TableInfo) string {
	kind := table.Kind
	if kind == "" {
		kind = CatalogKindTable
	}
	return kind
}

func tableStats(table datatype.TableInfo) map[string]interface{} {
	stats := map[string]interface{}{}
	if table.RowCount != nil {
		stats["row_count"] = *table.RowCount
	}
	if table.SizeBytes != nil {
		stats["size_bytes"] = *table.SizeBytes
	}
	return stats
}

func tableAttributes(namespace string, table datatype.TableInfo) map[string]interface{} {
	attrs := map[string]interface{}{
		"namespace": namespace,
	}
	if table.Name != "" {
		attrs["table"] = table.Name
	}
	if table.Comment != "" {
		attrs["comment"] = table.Comment
	}
	if table.UpdatedAt != nil {
		attrs["updated_at"] = table.UpdatedAt
	}
	if len(table.Native) > 0 {
		attrs["native"] = cloneInterfaceMap(table.Native)
	}
	return attrs
}

func cloneInterfaceMap(values map[string]interface{}) map[string]interface{} {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]interface{}, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func (a TabularCatalogCallbacks) validate() error {
	if a.ListNamespaces == nil || a.ListTables == nil || a.ListColumns == nil {
		return fmt.Errorf("tabular catalog callbacks is incomplete")
	}
	return nil
}

func (a TabularCatalogCallbacks) namespaceTerm() string {
	if a.NamespaceTerm != "" {
		return a.NamespaceTerm
	}
	return CatalogTermDatabase
}

func (a TabularCatalogCallbacks) isSystemNamespace(namespace string) bool {
	if a.IsSystemNamespaceFunc == nil {
		return false
	}
	return a.IsSystemNamespaceFunc(namespace)
}
