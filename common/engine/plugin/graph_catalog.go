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
			{Term: CatalogTermDatabase, Kinds: []string{CatalogKindNamespace}, Role: CatalogRoleBranch, I18nKey: CatalogTermI18nKey(CatalogTermDatabase)},
			{Term: CatalogTermGraph, Kinds: []string{CatalogKindGraph}, Role: CatalogRoleLeaf, I18nKey: CatalogTermI18nKey(CatalogTermGraph)},
		},
	}
}

type GraphCatalogCallbacks struct {
	ListNamespacesFunc   func(ctx context.Context, connInfo ConnectionInfo, root CatalogPath) ([]CatalogEntry, error)
	DescribeGraphFunc    func(ctx context.Context, connInfo ConnectionInfo, database string, opts CatalogFactsOptions) (*datatype.GraphInfo, error)
	IsSystemDatabaseFunc func(databaseName string) bool
}

func ListGraphCatalogChildren(ctx context.Context, callbacks GraphCatalogCallbacks, engineID uint, connInfo ConnectionInfo, parent CatalogPath, opts ListOptions) ([]CatalogEntry, error) {
	model := GraphCatalogModel()
	if IsCatalogRootPath(parent) {
		if err := requireCatalogRootPath(parent, model); err != nil {
			return nil, err
		}
		if callbacks.ListNamespacesFunc == nil {
			return nil, fmt.Errorf("graph catalog callbacks ListNamespacesFunc is nil")
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
	if _, err := requireCatalogBusinessPath(parent, model); err != nil {
		return nil, err
	}

	return []CatalogEntry{{
		Name: CatalogKindGraph,
		Path: appendCatalogSegment(parent, engineID, CatalogTermGraph, CatalogKindGraph, CatalogKindGraph),
		Term: CatalogTermGraph,
		Kind: CatalogKindGraph,
		Role: CatalogRoleLeaf,
	}}, nil
}

func (a GraphCatalogCallbacks) isSystemDatabase(databaseName string) bool {
	if a.IsSystemDatabaseFunc == nil {
		return false
	}
	return a.IsSystemDatabaseFunc(databaseName)
}

func ResolveGraphCatalogPath(ctx context.Context, callbacks GraphCatalogCallbacks, engineID uint, connInfo ConnectionInfo, path CatalogPath) (*CatalogEntry, error) {
	model := GraphCatalogModel()
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
	return &CatalogEntry{Name: last.Name, Path: path, Term: CatalogTermGraph, Kind: CatalogKindGraph, Role: CatalogRoleLeaf}, nil
}

func DescribeGraphCatalogFacts(ctx context.Context, callbacks GraphCatalogCallbacks, engineID uint, connInfo ConnectionInfo, path CatalogPath, opts CatalogFactsOptions) (*CatalogFacts, error) {
	segments, err := requireCatalogBusinessPath(path, GraphCatalogModel())
	if err != nil {
		return nil, err
	}
	if len(segments) < 2 {
		return nil, fmt.Errorf("graph item path requires database and graph segments")
	}
	if callbacks.DescribeGraphFunc == nil {
		return nil, fmt.Errorf("graph catalog callbacks DescribeGraphFunc is nil")
	}
	database := segments[0].Name
	graph, err := callbacks.DescribeGraphFunc(ctx, connInfo, database, opts)
	if err != nil {
		return nil, err
	}
	return &CatalogFacts{
		Path:  path,
		Kind:  CatalogKindGraph,
		Graph: graph.Clone(),
	}, nil
}
