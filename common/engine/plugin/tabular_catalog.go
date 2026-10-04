package plugin

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/common/datatype"
	"gorm.io/gorm"
)

const (
	EngineCatalogTermSchema   = "schema"
	EngineCatalogTermDatabase = "database"
	EngineCatalogTermTable    = "table"

	EngineCatalogKindNamespace = "namespace"
	EngineCatalogKindTable     = "table"
)

// TabularCatalogModel describes a tabular engine hierarchy.
func TabularCatalogModel(namespaceTerm string) EngineCatalogModelSpec {
	if namespaceTerm == "" {
		namespaceTerm = EngineCatalogTermDatabase
	}
	return EngineCatalogModelSpec{
		PathVersion: EngineCatalogPathVersion,
		RootTerm:    EngineCatalogTermServer,
		Levels: []EngineCatalogLevelSpec{
			{Term: namespaceTerm, Kinds: []string{EngineCatalogKindNamespace}, Role: EngineCatalogRoleBranch, I18nKey: EngineCatalogTermI18nKey(namespaceTerm)},
			{Term: EngineCatalogTermTable, Kinds: []string{EngineCatalogKindTable, "view", "materialized_view", "external_table"}, Role: EngineCatalogRoleLeaf, I18nKey: EngineCatalogTermI18nKey(EngineCatalogTermTable)},
		},
	}
}

type TabularCatalogCallbacks struct {
	NamespaceTerm         string
	ListNamespaces        func(ctx context.Context, db *gorm.DB, root EngineCatalogPath) ([]EngineCatalogEntry, error)
	ListTables            func(ctx context.Context, db *gorm.DB, namespace string) ([]datatype.TableInfo, error)
	GetTable              func(ctx context.Context, db *gorm.DB, namespace, table string) (*datatype.TableInfo, error)
	ListColumns           func(ctx context.Context, db *gorm.DB, namespace, table string) ([]datatype.FieldInfo, error)
	ListIndexes           func(ctx context.Context, db *gorm.DB, namespace, table string) ([]IndexFacts, error)
	ListConstraints       func(ctx context.Context, db *gorm.DB, namespace, table string) ([]ConstraintFacts, error)
	DescribePartitioning  func(ctx context.Context, db *gorm.DB, namespace, table string) (*TablePartitioningFacts, error)
	RowCount              func(ctx context.Context, db *gorm.DB, namespace, table string) (int64, error)
	DescribeSpatial       func(ctx context.Context, db *gorm.DB, namespace, table string, fields []datatype.FieldInfo) (*datatype.SpatialInfo, error)
	IsSystemNamespaceFunc func(namespace string) bool
}

// ListTabularCatalogChildren maps tabular callbacks to EngineCatalogProvider.
func ListTabularCatalogChildren(ctx context.Context, callbacks TabularCatalogCallbacks, engine *Engine, parent EngineCatalogPath, opts ListOptions) ([]EngineCatalogEntry, error) {
	if err := callbacks.validate(); err != nil {
		return nil, err
	}
	namespaceTerm := callbacks.namespaceTerm()
	model := TabularCatalogModel(namespaceTerm)
	if IsEngineCatalogRootPath(parent) {
		if err := requireCatalogRootPath(parent, model); err != nil {
			return nil, err
		}
		db, err := GetOrCreatePoolFromFactory(engine, DefaultPoolConfig())
		if err != nil {
			return nil, WrapEngineCatalogError(EngineCatalogErrorUnavailable, fmt.Errorf("create catalog connection pool: %w", err))
		}
		namespaces, err := callbacks.ListNamespaces(ctx, db, parent)
		if err != nil {
			return nil, err
		}
		nodes := make([]EngineCatalogEntry, 0, len(namespaces))
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
		return nil, WrapEngineCatalogError(EngineCatalogErrorUnavailable, fmt.Errorf("create catalog connection pool: %w", err))
	}
	tables, err := callbacks.ListTables(ctx, db, namespace)
	if err != nil {
		return nil, err
	}
	nodes := make([]EngineCatalogEntry, 0, len(tables))
	for _, table := range tables {
		path := appendCatalogSegment(parent, engine.ID, EngineCatalogTermTable, EngineCatalogKindTable, table.Name)
		nodes = append(nodes, *tabularCatalogEntry(path, &table))
	}
	return nodes, nil
}

// ResolveTabularCatalogPath resolves a namespace or table node.
func ResolveTabularCatalogPath(ctx context.Context, callbacks TabularCatalogCallbacks, engine *Engine, path EngineCatalogPath) (*EngineCatalogEntry, error) {
	if err := callbacks.validate(); err != nil {
		return nil, err
	}
	namespaceTerm := callbacks.namespaceTerm()
	model := TabularCatalogModel(namespaceTerm)
	if IsEngineCatalogRootPath(path) {
		if err := requireCatalogRootPath(path, model); err != nil {
			return nil, err
		}
		return &EngineCatalogEntry{Name: "", Path: path, Term: model.RootTerm, Kind: model.RootTerm, Role: EngineCatalogRoleBranch}, nil
	}

	segments, err := requireCatalogBusinessPath(path, model)
	if err != nil {
		return nil, err
	}
	last := segments[len(segments)-1]
	if len(segments) == 1 {
		return &EngineCatalogEntry{
			Name: last.Name,
			Path: path,
			Term: namespaceTerm,
			Kind: EngineCatalogKindNamespace,
			Role: EngineCatalogRoleBranch,
		}, nil
	}

	db, err := GetOrCreatePoolFromFactory(engine, DefaultPoolConfig())
	if err != nil {
		return nil, WrapEngineCatalogError(EngineCatalogErrorUnavailable, fmt.Errorf("create catalog connection pool: %w", err))
	}
	table, err := lookupTabularTable(ctx, callbacks, db, segments[0].Name, last.Name)
	if err != nil {
		return nil, err
	}
	return tabularCatalogEntry(path, table), nil
}

// DescribeTabularCatalogFacts maps tabular column callbacks and table stats to EngineCatalogFactsProvider.
func DescribeTabularCatalogFacts(ctx context.Context, callbacks TabularCatalogCallbacks, engine *Engine, path EngineCatalogPath, opts EngineCatalogFactsOptions) (*EngineCatalogFacts, error) {
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
	summary, err := lookupTabularTable(ctx, callbacks, db, namespace, table)
	if err != nil {
		return nil, err
	}
	tableInfo := *summary
	columns, err := callbacks.ListColumns(ctx, db, namespace, table)
	if err != nil {
		return nil, err
	}

	fields := NormalizeFieldInfos(columns)

	kind := tableCatalogKind(tableInfo)
	updatedAt := tableInfo.UpdatedAt
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
	facts := buildTabularCatalogFacts(path, namespace, table, fields, tableInfo, kind, updatedAt, spatialInfo)
	if opts.IncludeIndexes && callbacks.ListIndexes != nil {
		facts.Indexes, err = callbacks.ListIndexes(ctx, db, namespace, table)
		if err != nil {
			return nil, err
		}
	}
	if opts.IncludeConstraints && callbacks.ListConstraints != nil {
		facts.Constraints, err = callbacks.ListConstraints(ctx, db, namespace, table)
		if err != nil {
			return nil, err
		}
	}
	if opts.IncludePartitioning && callbacks.DescribePartitioning != nil {
		facts.Partitioning, err = callbacks.DescribePartitioning(ctx, db, namespace, table)
		if err != nil {
			return nil, err
		}
	}
	return facts, nil
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

func tabularCatalogEntry(path EngineCatalogPath, table *datatype.TableInfo) *EngineCatalogEntry {
	return &EngineCatalogEntry{
		Name: table.Name, Path: path, Term: EngineCatalogTermTable,
		Kind: tableCatalogKind(*table), Role: EngineCatalogRoleLeaf,
		Table: EngineCatalogEntryTableSummary(table), UpdatedAt: table.UpdatedAt,
	}
}

func buildTabularCatalogFacts(path EngineCatalogPath, namespace, table string, fields []datatype.FieldInfo, tableInfo datatype.TableInfo, kind string, updatedAt *time.Time, spatialInfo *datatype.SpatialInfo) *EngineCatalogFacts {
	fields = NormalizeFieldInfos(fields)
	if kind == "" {
		kind = EngineCatalogKindTable
	}
	if tableInfo.Kind != "" {
		kind = tableCatalogKind(tableInfo)
	}
	if updatedAt == nil {
		updatedAt = tableInfo.UpdatedAt
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

	return &EngineCatalogFacts{
		Path:      path,
		Kind:      kind,
		Table:     tableInfo.Clone(),
		Spatial:   spatialInfo.Clone(),
		UpdatedAt: updatedAt,
	}
}

func TabularNamespaceCatalogEntry(root EngineCatalogPath, namespaceTerm, name string, leafCount int) EngineCatalogEntry {
	if namespaceTerm == "" {
		namespaceTerm = EngineCatalogTermDatabase
	}
	return EngineCatalogEntry{
		Name:      name,
		Path:      appendCatalogSegment(root, root.EngineID, namespaceTerm, EngineCatalogKindNamespace, name),
		Term:      namespaceTerm,
		Kind:      EngineCatalogKindNamespace,
		Role:      EngineCatalogRoleBranch,
		LeafCount: &leafCount,
	}
}

func lookupTabularTable(ctx context.Context, callbacks TabularCatalogCallbacks, db *gorm.DB, namespace, tableName string) (*datatype.TableInfo, error) {
	if callbacks.isSystemNamespace(namespace) {
		return nil, WrapEngineCatalogError(EngineCatalogErrorNotFound, fmt.Errorf("catalog namespace %q is hidden", namespace))
	}
	table, err := callbacks.GetTable(ctx, db, namespace, tableName)
	if err != nil {
		return nil, err
	}
	if table == nil {
		return nil, WrapEngineCatalogError(EngineCatalogErrorNotFound, fmt.Errorf("catalog table %q not found in namespace %q", tableName, namespace))
	}
	if table.Name != tableName {
		return nil, fmt.Errorf("catalog lookup returned table %q for target %q", table.Name, tableName)
	}
	return table, nil
}

func tableCatalogKind(table datatype.TableInfo) string {
	kind := table.Kind
	if kind == "" {
		kind = EngineCatalogKindTable
	}
	return kind
}

func (a TabularCatalogCallbacks) validate() error {
	if a.ListNamespaces == nil || a.ListTables == nil || a.GetTable == nil || a.ListColumns == nil {
		return fmt.Errorf("tabular catalog callbacks is incomplete")
	}
	return nil
}

func (a TabularCatalogCallbacks) namespaceTerm() string {
	if a.NamespaceTerm != "" {
		return a.NamespaceTerm
	}
	return EngineCatalogTermDatabase
}

func (a TabularCatalogCallbacks) isSystemNamespace(namespace string) bool {
	if a.IsSystemNamespaceFunc == nil {
		return false
	}
	return a.IsSystemNamespaceFunc(namespace)
}
