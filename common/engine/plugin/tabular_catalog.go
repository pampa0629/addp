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
		RootTerm:    CatalogTermServer,
		Levels: []CatalogLevelSpec{
			{Term: namespaceTerm, Kinds: []string{CatalogKindNamespace}, Role: CatalogRoleBranch, I18nKey: CatalogTermI18nKey(namespaceTerm)},
			{Term: CatalogTermTable, Kinds: []string{CatalogKindTable, "view", "materialized_view", "external_table"}, Role: CatalogRoleLeaf, I18nKey: CatalogTermI18nKey(CatalogTermTable)},
		},
	}
}

type TabularCatalogCallbacks struct {
	NamespaceTerm         string
	ListNamespaces        func(ctx context.Context, db *gorm.DB, root CatalogPath) ([]CatalogEntry, error)
	ListTables            func(ctx context.Context, db *gorm.DB, namespace string) ([]datatype.TableInfo, error)
	ListColumns           func(ctx context.Context, db *gorm.DB, namespace, table string) ([]datatype.FieldInfo, error)
	RowCount              func(ctx context.Context, db *gorm.DB, namespace, table string) (int64, error)
	DescribeSpatial       func(ctx context.Context, db *gorm.DB, namespace, table string, fields []datatype.FieldInfo) (*datatype.SpatialInfo, error)
	IsSystemNamespaceFunc func(namespace string) bool
}

// ListTabularCatalogChildren maps tabular callbacks to CatalogProvider.
func ListTabularCatalogChildren(ctx context.Context, callbacks TabularCatalogCallbacks, engine *Engine, parent CatalogPath, opts ListOptions) ([]CatalogEntry, error) {
	if err := callbacks.validate(); err != nil {
		return nil, err
	}
	namespaceTerm := callbacks.namespaceTerm()
	model := TabularCatalogModel(namespaceTerm)
	if IsCatalogRootPath(parent) {
		if err := requireCatalogRootPath(parent, model); err != nil {
			return nil, err
		}
		db, err := GetOrCreatePoolFromFactory(engine, DefaultPoolConfig())
		if err != nil {
			return nil, WrapCatalogError(CatalogErrorUnavailable, fmt.Errorf("create catalog connection pool: %w", err))
		}
		namespaces, err := callbacks.ListNamespaces(ctx, db, parent)
		if err != nil {
			return nil, err
		}
		nodes := make([]CatalogEntry, 0, len(namespaces))
		for _, namespace := range namespaces {
			if callbacks.isSystemNamespace(namespace.Name) {
				continue
			}
			nodes = append(nodes, namespace)
		}
		return nodes, nil
	}

	segments, err := requireCatalogBusinessPath(parent, model)
	if err != nil {
		return nil, err
	}
	namespace := segments[0].Name
	db, err := GetOrCreatePoolFromFactory(engine, DefaultPoolConfig())
	if err != nil {
		return nil, WrapCatalogError(CatalogErrorUnavailable, fmt.Errorf("create catalog connection pool: %w", err))
	}
	tables, err := callbacks.ListTables(ctx, db, namespace)
	if err != nil {
		return nil, err
	}
	nodes := make([]CatalogEntry, 0, len(tables))
	for _, table := range tables {
		tableInfo := CatalogEntryTableSummary(&table)
		nodes = append(nodes, CatalogEntry{
			Name:      table.Name,
			Path:      appendCatalogSegment(parent, engine.ID, CatalogTermTable, CatalogKindTable, table.Name),
			Term:      CatalogTermTable,
			Kind:      tableCatalogKind(table),
			Role:      CatalogRoleLeaf,
			Table:     tableInfo,
			UpdatedAt: table.UpdatedAt,
		})
	}
	return nodes, nil
}

// ResolveTabularCatalogPath resolves a namespace or table node.
func ResolveTabularCatalogPath(ctx context.Context, callbacks TabularCatalogCallbacks, engine *Engine, path CatalogPath) (*CatalogEntry, error) {
	if err := callbacks.validate(); err != nil {
		return nil, err
	}
	namespaceTerm := callbacks.namespaceTerm()
	model := TabularCatalogModel(namespaceTerm)
	if IsCatalogRootPath(path) {
		if err := requireCatalogRootPath(path, model); err != nil {
			return nil, err
		}
		return &CatalogEntry{Name: "", Path: path, Term: model.RootTerm, Kind: model.RootTerm, Role: CatalogRoleBranch}, nil
	}

	segments, err := requireCatalogBusinessPath(path, model)
	if err != nil {
		return nil, err
	}
	last := segments[len(segments)-1]
	if len(segments) == 1 {
		return &CatalogEntry{
			Name: last.Name,
			Path: path,
			Term: namespaceTerm,
			Kind: CatalogKindNamespace,
			Role: CatalogRoleBranch,
		}, nil
	}

	facts, err := DescribeTabularCatalogFacts(ctx, callbacks, engine, path, CatalogFactsOptions{})
	if err != nil {
		return nil, err
	}
	kind := facts.Kind
	if kind == "" {
		kind = CatalogKindTable
	}
	return tabularCatalogEntryFromFacts(path, last.Name, kind, facts), nil
}

// DescribeTabularCatalogFacts maps tabular column callbacks and table stats to CatalogFactsProvider.
func DescribeTabularCatalogFacts(ctx context.Context, callbacks TabularCatalogCallbacks, engine *Engine, path CatalogPath, opts CatalogFactsOptions) (*CatalogFacts, error) {
	if err := callbacks.validate(); err != nil {
		return nil, err
	}
	segments, err := requireCatalogBusinessPath(path, TabularCatalogModel(callbacks.namespaceTerm()))
	if err != nil {
		return nil, err
	}
	if len(segments) < 2 {
		return nil, fmt.Errorf("tabular item path requires namespace and table segments")
	}

	db, err := GetOrCreatePoolFromFactory(engine, DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("获取连接池失败：%w", err)
	}

	namespace := segments[0].Name
	table := segments[len(segments)-1].Name
	columns, err := callbacks.ListColumns(ctx, db, namespace, table)
	if err != nil {
		return nil, err
	}

	fields := NormalizeFieldInfos(columns)

	tableInfo, hasTableInfo := findTableInfo(ctx, callbacks, db, namespace, table)
	kind := CatalogKindTable
	var updatedAt *time.Time
	if hasTableInfo {
		kind = tableCatalogKind(tableInfo)
		updatedAt = tableInfo.UpdatedAt
	}
	if opts.IncludeStatistics && callbacks.RowCount != nil {
		rowCount, err := callbacks.RowCount(ctx, db, namespace, table)
		if err == nil {
			tableInfo.RowCount = &rowCount
		}
	}
	var spatialInfo *datatype.SpatialInfo
	if opts.IncludeSpatialFacts && callbacks.DescribeSpatial != nil {
		spatialInfo, err = callbacks.DescribeSpatial(ctx, db, namespace, table, fields)
		if err != nil {
			return nil, err
		}
	}
	return buildTabularCatalogFacts(path, namespace, table, fields, tableInfo, hasTableInfo, kind, updatedAt, spatialInfo), nil
}

func primaryKeyFields(fields []datatype.FieldInfo) []string {
	if len(fields) == 0 {
		return nil
	}
	keys := make([]string, 0)
	for _, field := range fields {
		if field.PrimaryKey && field.Name != "" {
			keys = append(keys, field.Name)
		}
	}
	return keys
}

func tabularCatalogEntryFromFacts(path CatalogPath, name, kind string, facts *CatalogFacts) *CatalogEntry {
	if kind == "" {
		kind = CatalogKindTable
	}
	if facts == nil {
		return &CatalogEntry{
			Name: name,
			Path: path,
			Term: CatalogTermTable,
			Kind: kind,
			Role: CatalogRoleLeaf,
		}
	}
	return &CatalogEntry{
		Name:      name,
		Path:      path,
		Term:      CatalogTermTable,
		Kind:      kind,
		Role:      CatalogRoleLeaf,
		Table:     CatalogEntryTableInfo(facts),
		UpdatedAt: facts.UpdatedAt,
	}
}

func buildTabularCatalogFacts(path CatalogPath, namespace, table string, fields []datatype.FieldInfo, tableInfo datatype.TableInfo, hasTableInfo bool, kind string, updatedAt *time.Time, spatialInfo *datatype.SpatialInfo) *CatalogFacts {
	fields = NormalizeFieldInfos(fields)
	if kind == "" {
		kind = CatalogKindTable
	}
	if hasTableInfo {
		if tableInfo.Kind != "" {
			kind = tableCatalogKind(tableInfo)
		}
		if updatedAt == nil {
			updatedAt = tableInfo.UpdatedAt
		}
	}
	tableInfo.Name = table
	tableInfo.Kind = kind
	tableInfo.Fields = append([]datatype.FieldInfo(nil), fields...)
	if len(tableInfo.PrimaryKey) == 0 {
		tableInfo.PrimaryKey = primaryKeyFields(fields)
	}
	if len(tableInfo.Native) == 0 {
		tableInfo.Native = map[string]interface{}{}
	}
	if namespace != "" {
		tableInfo.Native["namespace"] = namespace
	}

	return &CatalogFacts{
		Path:      path,
		Kind:      kind,
		Table:     tableInfo.Clone(),
		Spatial:   spatialInfo.Clone(),
		UpdatedAt: updatedAt,
	}
}

func TabularNamespaceCatalogEntry(root CatalogPath, namespaceTerm, name string, leafCount int) CatalogEntry {
	if namespaceTerm == "" {
		namespaceTerm = CatalogTermDatabase
	}
	return CatalogEntry{
		Name:      name,
		Path:      appendCatalogSegment(root, root.EngineID, namespaceTerm, CatalogKindNamespace, name),
		Term:      namespaceTerm,
		Kind:      CatalogKindNamespace,
		Role:      CatalogRoleBranch,
		LeafCount: &leafCount,
	}
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
