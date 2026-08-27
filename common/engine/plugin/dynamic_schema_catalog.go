package plugin

import (
	"context"
	"fmt"

	"github.com/addp/common/datatype"
)

const (
	EngineCatalogTermCollection = "collection"

	EngineCatalogKindCollection = "collection"
)

func DynamicSchemaCatalogModel() EngineCatalogModelSpec {
	return EngineCatalogModelSpec{
		PathVersion: EngineCatalogPathVersion,
		RootTerm:    EngineCatalogTermServer,
		Levels: []EngineCatalogLevelSpec{
			{Term: EngineCatalogTermDatabase, Kinds: []string{EngineCatalogKindNamespace}, Role: EngineCatalogRoleBranch, I18nKey: EngineCatalogTermI18nKey(EngineCatalogTermDatabase)},
			{Term: EngineCatalogTermCollection, Kinds: []string{EngineCatalogKindCollection}, Role: EngineCatalogRoleLeaf, I18nKey: EngineCatalogTermI18nKey(EngineCatalogTermCollection)},
		},
	}
}

type DynamicSchemaCatalogCallbacks struct {
	ListNamespacesFunc     func(ctx context.Context, connInfo ConnectionInfo, root EngineCatalogPath) ([]EngineCatalogEntry, error)
	ListCollectionsFunc    func(ctx context.Context, connInfo ConnectionInfo, parent EngineCatalogPath, database string) ([]EngineCatalogEntry, error)
	DescribeCollectionFunc func(ctx context.Context, connInfo ConnectionInfo, database, collection string) (*DynamicCollectionFacts, error)
	IsSystemDatabaseFunc   func(databaseName string) bool
}

func ListDynamicSchemaCatalogChildren(ctx context.Context, callbacks DynamicSchemaCatalogCallbacks, engineID uint, connInfo ConnectionInfo, parent EngineCatalogPath, opts ListOptions) ([]EngineCatalogEntry, error) {
	model := DynamicSchemaCatalogModel()
	if IsEngineCatalogRootPath(parent) {
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
		nodes := make([]EngineCatalogEntry, 0, len(namespaces))
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

func ResolveDynamicSchemaCatalogPath(ctx context.Context, callbacks DynamicSchemaCatalogCallbacks, engineID uint, connInfo ConnectionInfo, path EngineCatalogPath) (*EngineCatalogEntry, error) {
	model := DynamicSchemaCatalogModel()
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
		return &EngineCatalogEntry{Name: last.Name, Path: path, Term: EngineCatalogTermDatabase, Kind: EngineCatalogKindNamespace, Role: EngineCatalogRoleBranch}, nil
	}
	facts, err := DescribeDynamicSchemaCatalogFacts(ctx, callbacks, engineID, connInfo, path, EngineCatalogFactsOptions{})
	if err != nil {
		return nil, err
	}
	return &EngineCatalogEntry{Name: last.Name, Path: path, Term: EngineCatalogTermCollection, Kind: EngineCatalogKindCollection, Role: EngineCatalogRoleLeaf, Table: EngineCatalogEntryTableInfo(facts)}, nil
}

func DescribeDynamicSchemaCatalogFacts(ctx context.Context, callbacks DynamicSchemaCatalogCallbacks, engineID uint, connInfo ConnectionInfo, path EngineCatalogPath, opts EngineCatalogFactsOptions) (*EngineCatalogFacts, error) {
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
	return &EngineCatalogFacts{
		Path: path,
		Kind: EngineCatalogKindCollection,
		Table: &datatype.TableInfo{
			Name:              collection,
			Kind:              EngineCatalogKindCollection,
			EstimatedRowCount: stats.DocumentCount,
			SizeBytes:         &stats.SizeBytes,
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

func DynamicCollectionCatalogEntry(parent EngineCatalogPath, database, name string, facts DynamicCollectionFacts) EngineCatalogEntry {
	sizeBytes := facts.SizeBytes
	return EngineCatalogEntry{
		Name: name,
		Path: appendCatalogSegment(parent, parent.EngineID, EngineCatalogTermCollection, EngineCatalogKindCollection, name),
		Term: EngineCatalogTermCollection,
		Kind: EngineCatalogKindCollection,
		Role: EngineCatalogRoleLeaf,
		Table: EngineCatalogEntryTableSummary(&datatype.TableInfo{
			Name:              name,
			Kind:              EngineCatalogKindCollection,
			EstimatedRowCount: facts.DocumentCount,
			SizeBytes:         &sizeBytes,
			Native: map[string]interface{}{
				"database": database,
			},
		}),
	}
}
