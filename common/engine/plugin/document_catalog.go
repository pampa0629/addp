package plugin

import (
	"context"
	"fmt"
)

const (
	CatalogTermCollection = "collection"

	CatalogKindCollection = "collection"
)

func DocumentCatalogModel() CatalogModelSpec {
	return CatalogModelSpec{
		PathVersion: CatalogPathVersion,
		RootTerm:    "server",
		Levels: []CatalogLevelSpec{
			{Term: CatalogTermDatabase, Kinds: []string{CatalogKindNamespace}, Container: true, I18nKey: CatalogTermI18nKey(CatalogTermDatabase)},
			{Term: CatalogTermCollection, Kinds: []string{CatalogKindCollection}, Item: true, I18nKey: CatalogTermI18nKey(CatalogTermCollection)},
		},
	}
}

type DocumentCatalogCallbacks struct {
	ListDatabasesFunc      func(ctx context.Context, connInfo ConnectionInfo) ([]DatabaseInfo, error)
	ListCollectionsFunc    func(ctx context.Context, connInfo ConnectionInfo, database string) ([]CollectionInfo, error)
	GetCollectionStatsFunc func(ctx context.Context, connInfo ConnectionInfo, database, collection string) (*CollectionStats, error)
	IsSystemDatabaseFunc   func(databaseName string) bool
}

func ListDocumentCatalogChildren(ctx context.Context, callbacks DocumentCatalogCallbacks, engineID uint, connInfo ConnectionInfo, parent CatalogPath, opts ListOptions) ([]CatalogNode, error) {
	if len(parent.Segments) == 0 {
		if callbacks.ListDatabasesFunc == nil {
			return nil, fmt.Errorf("document catalog callbacks ListDatabasesFunc is nil")
		}
		databases, err := callbacks.ListDatabasesFunc(ctx, connInfo)
		if err != nil {
			return nil, err
		}
		nodes := make([]CatalogNode, 0, len(databases))
		for _, db := range databases {
			if callbacks.isSystemDatabase(db.Name) {
				continue
			}
			nodes = append(nodes, CatalogNode{
				Name:        db.Name,
				Path:        appendCatalogSegment(parent, engineID, CatalogTermDatabase, CatalogKindNamespace, db.Name),
				Term:        CatalogTermDatabase,
				Kind:        CatalogKindNamespace,
				IsContainer: true,
				Stats: map[string]interface{}{
					"size_bytes": db.SizeBytes,
				},
			})
		}
		return nodes, nil
	}

	database := parent.Segments[0].Name
	if callbacks.ListCollectionsFunc == nil {
		return nil, fmt.Errorf("document catalog callbacks ListCollectionsFunc is nil")
	}
	collections, err := callbacks.ListCollectionsFunc(ctx, connInfo, database)
	if err != nil {
		return nil, err
	}
	nodes := make([]CatalogNode, 0, len(collections))
	for _, coll := range collections {
		nodes = append(nodes, CatalogNode{
			Name:   coll.Name,
			Path:   appendCatalogSegment(parent, engineID, CatalogTermCollection, CatalogKindCollection, coll.Name),
			Term:   CatalogTermCollection,
			Kind:   CatalogKindCollection,
			IsItem: true,
			Stats: map[string]interface{}{
				"document_count": coll.DocumentCount,
				"size_bytes":     coll.SizeBytes,
			},
			Attributes: map[string]interface{}{"database": database},
		})
	}
	return nodes, nil
}

func (a DocumentCatalogCallbacks) isSystemDatabase(databaseName string) bool {
	if a.IsSystemDatabaseFunc == nil {
		return false
	}
	return a.IsSystemDatabaseFunc(databaseName)
}

func ResolveDocumentCatalogPath(ctx context.Context, callbacks DocumentCatalogCallbacks, engineID uint, connInfo ConnectionInfo, path CatalogPath) (*CatalogNode, error) {
	if len(path.Segments) == 0 {
		return &CatalogNode{Name: "", Path: CatalogPath{Version: CatalogPathVersion, EngineID: engineID}, Term: "server", Kind: "server", IsContainer: true}, nil
	}
	last := path.Segments[len(path.Segments)-1]
	if len(path.Segments) == 1 {
		return &CatalogNode{Name: last.Name, Path: path, Term: CatalogTermDatabase, Kind: CatalogKindNamespace, IsContainer: true}, nil
	}
	meta, err := DescribeDocumentItem(ctx, callbacks, engineID, connInfo, path, MetadataOptions{})
	if err != nil {
		return nil, err
	}
	return &CatalogNode{Name: last.Name, Path: path, Term: CatalogTermCollection, Kind: CatalogKindCollection, IsItem: true, Stats: meta.Stats}, nil
}

func DescribeDocumentItem(ctx context.Context, callbacks DocumentCatalogCallbacks, engineID uint, connInfo ConnectionInfo, path CatalogPath, opts MetadataOptions) (*ItemMetadata, error) {
	if len(path.Segments) < 2 {
		return nil, fmt.Errorf("collection item path requires database and collection segments")
	}
	if callbacks.GetCollectionStatsFunc == nil {
		return nil, fmt.Errorf("document catalog callbacks GetCollectionStatsFunc is nil")
	}
	database := path.Segments[0].Name
	collection := path.Segments[len(path.Segments)-1].Name
	stats, err := callbacks.GetCollectionStatsFunc(ctx, connInfo, database, collection)
	if err != nil {
		return nil, err
	}
	indexes := make([]IndexInfo, 0, len(stats.Indexes))
	for _, idx := range stats.Indexes {
		indexes = append(indexes, idx)
	}
	return &ItemMetadata{
		Path:    path,
		Kind:    CatalogKindCollection,
		Indexes: indexes,
		Stats: map[string]interface{}{
			"document_count": stats.DocumentCount,
			"size_bytes":     stats.SizeBytes,
			"index_count":    stats.IndexCount,
			"avg_doc_size":   stats.AvgDocSize,
		},
		Attributes: map[string]interface{}{
			"database":   database,
			"collection": collection,
		},
	}, nil
}
