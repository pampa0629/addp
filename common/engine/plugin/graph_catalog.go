package plugin

import (
	"context"
	"fmt"

	"github.com/addp/common/datatype"
)

const (
	CatalogTermGraph = "graph"

	CatalogKindGraph = "graph"
)

func GraphCatalogModel() CatalogModelSpec {
	return CatalogModelSpec{
		PathVersion: CatalogPathVersion,
		RootTerm:    "server",
		Levels: []CatalogLevelSpec{
			{Term: CatalogTermDatabase, Kinds: []string{CatalogKindNamespace}, Container: true, I18nKey: CatalogTermI18nKey(CatalogTermDatabase)},
			{Term: CatalogTermGraph, Kinds: []string{CatalogKindGraph}, Item: true, I18nKey: CatalogTermI18nKey(CatalogTermGraph)},
		},
	}
}

type GraphCatalogCallbacks struct {
	ListDatabasesFunc    func(ctx context.Context, connInfo ConnectionInfo) ([]DatabaseInfo, error)
	DescribeGraphFunc    func(ctx context.Context, connInfo ConnectionInfo, database string, opts MetadataOptions) (*datatype.GraphInfo, error)
	IsSystemDatabaseFunc func(databaseName string) bool
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

	return []CatalogNode{{
		Name:   CatalogKindGraph,
		Path:   appendCatalogSegment(parent, engineID, CatalogTermGraph, CatalogKindGraph, CatalogKindGraph),
		Term:   CatalogTermGraph,
		Kind:   CatalogKindGraph,
		IsItem: true,
	}}, nil
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
	return &CatalogNode{Name: last.Name, Path: path, Term: CatalogTermGraph, Kind: CatalogKindGraph, IsItem: true}, nil
}

func DescribeGraphItem(ctx context.Context, callbacks GraphCatalogCallbacks, engineID uint, connInfo ConnectionInfo, path CatalogPath, opts MetadataOptions) (*ItemMetadata, error) {
	if len(path.Segments) < 2 {
		return nil, fmt.Errorf("graph item path requires database and graph segments")
	}
	if callbacks.DescribeGraphFunc == nil {
		return nil, fmt.Errorf("graph catalog callbacks DescribeGraphFunc is nil")
	}
	database := path.Segments[0].Name
	graph, err := callbacks.DescribeGraphFunc(ctx, connInfo, database, opts)
	if err != nil {
		return nil, err
	}
	return &ItemMetadata{
		Path:  path,
		Kind:  CatalogKindGraph,
		Graph: graph.Clone(),
		Attributes: map[string]interface{}{
			"database": database,
			"name":     CatalogKindGraph,
			"term":     CatalogTermGraph,
		},
	}, nil
}
