package plugin

import (
	"context"
	"fmt"
)

const (
	CatalogTermLabel        = "label"
	CatalogTermRelationship = "relationship"

	CatalogKindLabel        = "label"
	CatalogKindRelationship = "relationship"
)

func GraphCatalogModel() CatalogModelSpec {
	return CatalogModelSpec{
		PathVersion: CatalogPathVersion,
		RootTerm:    "server",
		Levels: []CatalogLevelSpec{
			{Term: CatalogTermDatabase, Kinds: []string{CatalogKindNamespace}, Container: true, I18nKey: CatalogTermI18nKey(CatalogTermDatabase)},
			{Term: CatalogTermLabel, Kinds: []string{CatalogKindLabel, CatalogKindRelationship}, Item: true, I18nKey: CatalogTermI18nKey(CatalogTermLabel)},
		},
	}
}

type GraphCatalogCallbacks struct {
	ListDatabasesFunc         func(ctx context.Context, connInfo ConnectionInfo) ([]DatabaseInfo, error)
	ListNodeLabelsFunc        func(ctx context.Context, connInfo ConnectionInfo, database string) ([]NodeLabelInfo, error)
	ListRelationshipTypesFunc func(ctx context.Context, connInfo ConnectionInfo, database string) ([]RelationshipTypeInfo, error)
	IsSystemDatabaseFunc      func(databaseName string) bool
}

func ListGraphCatalogChildren(ctx context.Context, callbacks GraphCatalogCallbacks, engineID uint, connInfo ConnectionInfo, parent CatalogPath, opts ListOptions) ([]CatalogNode, error) {
	if len(parent.Segments) == 0 {
		if callbacks.ListDatabasesFunc == nil {
			return nil, fmt.Errorf("graph catalog callbacks ListDatabasesFunc is nil")
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
			nodes = append(nodes, CatalogNode{Name: db.Name, Path: appendCatalogSegment(parent, engineID, CatalogTermDatabase, CatalogKindNamespace, db.Name), Term: CatalogTermDatabase, Kind: CatalogKindNamespace, IsContainer: true})
		}
		return nodes, nil
	}

	database := parent.Segments[0].Name
	if callbacks.ListNodeLabelsFunc == nil {
		return nil, fmt.Errorf("graph catalog callbacks ListNodeLabelsFunc is nil")
	}
	labels, err := callbacks.ListNodeLabelsFunc(ctx, connInfo, database)
	if err != nil {
		return nil, err
	}
	if callbacks.ListRelationshipTypesFunc == nil {
		return nil, fmt.Errorf("graph catalog callbacks ListRelationshipTypesFunc is nil")
	}
	rels, err := callbacks.ListRelationshipTypesFunc(ctx, connInfo, database)
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

func (a GraphCatalogCallbacks) isSystemDatabase(databaseName string) bool {
	if a.IsSystemDatabaseFunc == nil {
		return false
	}
	return a.IsSystemDatabaseFunc(databaseName)
}

func ResolveGraphCatalogPath(ctx context.Context, callbacks GraphCatalogCallbacks, engineID uint, connInfo ConnectionInfo, path CatalogPath) (*CatalogNode, error) {
	if len(path.Segments) == 0 {
		return &CatalogNode{Name: "", Path: CatalogPath{Version: CatalogPathVersion, EngineID: engineID}, Term: "server", Kind: "server", IsContainer: true}, nil
	}
	last := path.Segments[len(path.Segments)-1]
	if len(path.Segments) == 1 {
		return &CatalogNode{Name: last.Name, Path: path, Term: CatalogTermDatabase, Kind: CatalogKindNamespace, IsContainer: true}, nil
	}
	return &CatalogNode{Name: last.Name, Path: path, Term: last.Term, Kind: last.Kind, IsItem: true}, nil
}

func DescribeGraphItem(ctx context.Context, engineID uint, connInfo ConnectionInfo, path CatalogPath, opts MetadataOptions) (*ItemMetadata, error) {
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
