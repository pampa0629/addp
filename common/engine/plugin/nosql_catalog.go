package plugin

import (
	"context"
	"fmt"
)

const (
	CatalogTermCollection   = "collection"
	CatalogTermLabel        = "label"
	CatalogTermRelationship = "relationship"

	CatalogKindCollection   = "collection"
	CatalogKindLabel        = "label"
	CatalogKindRelationship = "relationship"
)

func DocumentCatalogModel() CatalogModelSpec {
	return CatalogModelSpec{
		PathVersion: CatalogPathVersion,
		RootTerm:    "server",
		Levels: []CatalogLevelSpec{
			{Term: CatalogTermDatabase, Kinds: []string{CatalogKindNamespace}, Container: true},
			{Term: CatalogTermCollection, Kinds: []string{CatalogKindCollection}, Item: true},
		},
	}
}

func GraphCatalogModel() CatalogModelSpec {
	return CatalogModelSpec{
		PathVersion: CatalogPathVersion,
		RootTerm:    "server",
		Levels: []CatalogLevelSpec{
			{Term: CatalogTermDatabase, Kinds: []string{CatalogKindNamespace}, Container: true},
			{Term: CatalogTermLabel, Kinds: []string{CatalogKindLabel, CatalogKindRelationship}, Item: true},
		},
	}
}

func ListDocumentCatalogChildren(ctx context.Context, docPlugin DocumentDBPlugin, engineID uint, connInfo ConnectionInfo, parent CatalogPath, opts ListOptions) ([]CatalogNode, error) {
	if len(parent.Segments) == 0 {
		databases, err := docPlugin.ListDatabases(ctx, connInfo)
		if err != nil {
			return nil, err
		}
		nodes := make([]CatalogNode, 0, len(databases))
		for _, db := range databases {
			if docPlugin.IsSystemDatabase(db.Name) {
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
	collections, err := docPlugin.ListCollections(ctx, connInfo, database)
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

func ResolveDocumentCatalogPath(ctx context.Context, docPlugin DocumentDBPlugin, engineID uint, connInfo ConnectionInfo, path CatalogPath) (*CatalogNode, error) {
	if len(path.Segments) == 0 {
		return &CatalogNode{Name: "", Path: CatalogPath{Version: CatalogPathVersion, EngineID: engineID}, Term: "server", Kind: "server", IsContainer: true}, nil
	}
	last := path.Segments[len(path.Segments)-1]
	if len(path.Segments) == 1 {
		return &CatalogNode{Name: last.Name, Path: path, Term: CatalogTermDatabase, Kind: CatalogKindNamespace, IsContainer: true}, nil
	}
	meta, err := DescribeDocumentItem(ctx, docPlugin, engineID, connInfo, path, MetadataOptions{})
	if err != nil {
		return nil, err
	}
	return &CatalogNode{Name: last.Name, Path: path, Term: CatalogTermCollection, Kind: CatalogKindCollection, IsItem: true, Stats: meta.Stats}, nil
}

func DescribeDocumentItem(ctx context.Context, docPlugin DocumentDBPlugin, engineID uint, connInfo ConnectionInfo, path CatalogPath, opts MetadataOptions) (*ItemMetadata, error) {
	if len(path.Segments) < 2 {
		return nil, fmt.Errorf("document item path requires database and collection segments")
	}
	database := path.Segments[0].Name
	collection := path.Segments[len(path.Segments)-1].Name
	stats, err := docPlugin.GetCollectionStats(ctx, connInfo, database, collection)
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

func ListGraphCatalogChildren(ctx context.Context, graphPlugin GraphDBPlugin, engineID uint, connInfo ConnectionInfo, parent CatalogPath, opts ListOptions) ([]CatalogNode, error) {
	if len(parent.Segments) == 0 {
		databases, err := graphPlugin.ListDatabases(ctx, connInfo)
		if err != nil {
			return nil, err
		}
		nodes := make([]CatalogNode, 0, len(databases))
		for _, db := range databases {
			if graphPlugin.IsSystemDatabase(db.Name) {
				continue
			}
			nodes = append(nodes, CatalogNode{Name: db.Name, Path: appendCatalogSegment(parent, engineID, CatalogTermDatabase, CatalogKindNamespace, db.Name), Term: CatalogTermDatabase, Kind: CatalogKindNamespace, IsContainer: true})
		}
		return nodes, nil
	}

	database := parent.Segments[0].Name
	labels, err := graphPlugin.ListNodeLabels(ctx, connInfo, database)
	if err != nil {
		return nil, err
	}
	rels, err := graphPlugin.ListRelationshipTypes(ctx, connInfo, database)
	if err != nil {
		return nil, err
	}
	nodes := make([]CatalogNode, 0, len(labels)+len(rels))
	for _, label := range labels {
		nodes = append(nodes, CatalogNode{Name: label.Name, Path: appendCatalogSegment(parent, engineID, CatalogTermLabel, CatalogKindLabel, label.Name), Term: CatalogTermLabel, Kind: CatalogKindLabel, IsItem: true, Stats: map[string]interface{}{"count": label.Count}})
	}
	for _, rel := range rels {
		nodes = append(nodes, CatalogNode{Name: rel.Name, Path: appendCatalogSegment(parent, engineID, CatalogTermRelationship, CatalogKindRelationship, rel.Name), Term: CatalogTermRelationship, Kind: CatalogKindRelationship, IsItem: true, Stats: map[string]interface{}{"count": rel.Count}, Attributes: map[string]interface{}{"from_labels": rel.FromLabels, "to_labels": rel.ToLabels}})
	}
	return nodes, nil
}

func ResolveGraphCatalogPath(ctx context.Context, graphPlugin GraphDBPlugin, engineID uint, connInfo ConnectionInfo, path CatalogPath) (*CatalogNode, error) {
	if len(path.Segments) == 0 {
		return &CatalogNode{Name: "", Path: CatalogPath{Version: CatalogPathVersion, EngineID: engineID}, Term: "server", Kind: "server", IsContainer: true}, nil
	}
	last := path.Segments[len(path.Segments)-1]
	if len(path.Segments) == 1 {
		return &CatalogNode{Name: last.Name, Path: path, Term: CatalogTermDatabase, Kind: CatalogKindNamespace, IsContainer: true}, nil
	}
	return &CatalogNode{Name: last.Name, Path: path, Term: last.Term, Kind: last.Kind, IsItem: true}, nil
}

func DescribeGraphItem(ctx context.Context, graphPlugin GraphDBPlugin, engineID uint, connInfo ConnectionInfo, path CatalogPath, opts MetadataOptions) (*ItemMetadata, error) {
	if len(path.Segments) < 2 {
		return nil, fmt.Errorf("graph item path requires database and label/relationship segments")
	}
	database := path.Segments[0].Name
	item := path.Segments[len(path.Segments)-1]
	return &ItemMetadata{
		Path: path,
		Kind: item.Kind,
		Attributes: map[string]interface{}{
			"database": database,
			"name":     item.Name,
			"term":     item.Term,
		},
	}, nil
}
