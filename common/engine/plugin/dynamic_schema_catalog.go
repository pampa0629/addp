package plugin

import (
	"context"
	"fmt"

	"github.com/addp/common/datatype"
)

const (
	CatalogTermCollection = "collection"

	CatalogKindCollection = "collection"
)

func DynamicSchemaCatalogModel() CatalogModelSpec {
	return CatalogModelSpec{
		PathVersion: CatalogPathVersion,
		RootTerm:    CatalogTermServer,
		Levels: []CatalogLevelSpec{
			{Term: CatalogTermDatabase, Kinds: []string{CatalogKindNamespace}, Role: CatalogRoleBranch, I18nKey: CatalogTermI18nKey(CatalogTermDatabase)},
			{Term: CatalogTermCollection, Kinds: []string{CatalogKindCollection}, Role: CatalogRoleLeaf, I18nKey: CatalogTermI18nKey(CatalogTermCollection)},
		},
	}
}

type DynamicSchemaCatalogCallbacks struct {
	ListNamespacesFunc     func(ctx context.Context, connInfo ConnectionInfo, root CatalogPath) ([]CatalogEntry, error)
	ListCollectionsFunc    func(ctx context.Context, connInfo ConnectionInfo, parent CatalogPath, database string) ([]CatalogEntry, error)
	DescribeCollectionFunc func(ctx context.Context, connInfo ConnectionInfo, database, collection string) (*DynamicCollectionFacts, error)
	IsSystemDatabaseFunc   func(databaseName string) bool
}

func ListDynamicSchemaCatalogChildren(ctx context.Context, callbacks DynamicSchemaCatalogCallbacks, engineID uint, connInfo ConnectionInfo, parent CatalogPath, opts ListOptions) ([]CatalogEntry, error) {
	model := DynamicSchemaCatalogModel()
	if IsCatalogRootPath(parent) {
		if err := requireCatalogRootPath(parent, model); err != nil {
			return nil, err
		}
		if callbacks.ListNamespacesFunc == nil {
			return nil, fmt.Errorf("dynamic schema catalog callbacks ListNamespacesFunc is nil")
		}
		namespaces, err := callbacks.ListNamespacesFunc(ctx, connInfo, parent)
		if err != nil {
			return nil, err
		}
		nodes := make([]CatalogEntry, 0, len(namespaces))
		for _, namespace := range namespaces {
			if callbacks.isSystemDatabase(namespace.Name) {
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
	database := segments[0].Name
	if callbacks.ListCollectionsFunc == nil {
		return nil, fmt.Errorf("dynamic schema catalog callbacks ListCollectionsFunc is nil")
	}
	collections, err := callbacks.ListCollectionsFunc(ctx, connInfo, parent, database)
	if err != nil {
		return nil, err
	}
	return collections, nil
}

func (a DynamicSchemaCatalogCallbacks) isSystemDatabase(databaseName string) bool {
	if a.IsSystemDatabaseFunc == nil {
		return false
	}
	return a.IsSystemDatabaseFunc(databaseName)
}

func ResolveDynamicSchemaCatalogPath(ctx context.Context, callbacks DynamicSchemaCatalogCallbacks, engineID uint, connInfo ConnectionInfo, path CatalogPath) (*CatalogEntry, error) {
	model := DynamicSchemaCatalogModel()
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
		return &CatalogEntry{Name: last.Name, Path: path, Term: CatalogTermDatabase, Kind: CatalogKindNamespace, Role: CatalogRoleBranch}, nil
	}
	facts, err := DescribeDynamicSchemaCatalogFacts(ctx, callbacks, engineID, connInfo, path, CatalogFactsOptions{})
	if err != nil {
		return nil, err
	}
	return &CatalogEntry{Name: last.Name, Path: path, Term: CatalogTermCollection, Kind: CatalogKindCollection, Role: CatalogRoleLeaf, Table: CatalogEntryTableInfo(facts)}, nil
}

func DescribeDynamicSchemaCatalogFacts(ctx context.Context, callbacks DynamicSchemaCatalogCallbacks, engineID uint, connInfo ConnectionInfo, path CatalogPath, opts CatalogFactsOptions) (*CatalogFacts, error) {
	segments, err := requireCatalogBusinessPath(path, DynamicSchemaCatalogModel())
	if err != nil {
		return nil, err
	}
	if len(segments) < 2 {
		return nil, fmt.Errorf("collection item path requires database and collection segments")
	}
	if callbacks.DescribeCollectionFunc == nil {
		return nil, fmt.Errorf("dynamic schema catalog callbacks DescribeCollectionFunc is nil")
	}
	database := segments[0].Name
	collection := segments[len(segments)-1].Name
	stats, err := callbacks.DescribeCollectionFunc(ctx, connInfo, database, collection)
	if err != nil {
		return nil, err
	}
	indexes := make([]IndexFacts, 0, len(stats.Indexes))
	for _, idx := range stats.Indexes {
		indexes = append(indexes, idx)
	}
	return &CatalogFacts{
		Path: path,
		Kind: CatalogKindCollection,
		Table: &datatype.TableInfo{
			Name:      collection,
			Kind:      CatalogKindCollection,
			RowCount:  &stats.DocumentCount,
			SizeBytes: &stats.SizeBytes,
			Native: map[string]interface{}{
				"database":        database,
				"collection":      collection,
				"index_count":     stats.IndexCount,
				"avg_record_size": stats.AvgRecordSize,
				"schema_type":     "dynamic",
			},
		},
		Indexes: indexes,
	}, nil
}

func DynamicCollectionCatalogEntry(parent CatalogPath, database, name string, facts DynamicCollectionFacts) CatalogEntry {
	rowCount := facts.DocumentCount
	sizeBytes := facts.SizeBytes
	return CatalogEntry{
		Name: name,
		Path: appendCatalogSegment(parent, parent.EngineID, CatalogTermCollection, CatalogKindCollection, name),
		Term: CatalogTermCollection,
		Kind: CatalogKindCollection,
		Role: CatalogRoleLeaf,
		Table: CatalogEntryTableSummary(&datatype.TableInfo{
			Name:      name,
			Kind:      CatalogKindCollection,
			RowCount:  &rowCount,
			SizeBytes: &sizeBytes,
			Native: map[string]interface{}{
				"database": database,
			},
		}),
	}
}
