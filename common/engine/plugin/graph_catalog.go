package plugin

import (
	"context"
	"fmt"

	"github.com/addp/common/datatype"
)

const (
	EngineCatalogTermGraph = "graph"

	EngineCatalogKindGraph = "graph"
)

func GraphCatalogModel() EngineCatalogModelSpec {
	return EngineCatalogModelSpec{
		PathVersion: EngineCatalogPathVersion,
		RootTerm:    EngineCatalogTermServer,
		Levels: []EngineCatalogLevelSpec{
			{Term: EngineCatalogTermDatabase, Kinds: []string{EngineCatalogKindNamespace}, Role: EngineCatalogRoleBranch, I18nKey: EngineCatalogTermI18nKey(EngineCatalogTermDatabase)},
			{Term: EngineCatalogTermGraph, Kinds: []string{EngineCatalogKindGraph}, Role: EngineCatalogRoleLeaf, I18nKey: EngineCatalogTermI18nKey(EngineCatalogTermGraph)},
		},
	}
}

type GraphCatalogCallbacks struct {
	ListNamespacesFunc   func(ctx context.Context, connInfo ConnectionInfo, root EngineCatalogPath) ([]EngineCatalogEntry, error)
	DescribeGraphFunc    func(ctx context.Context, connInfo ConnectionInfo, database string, opts EngineCatalogFactsOptions) (*datatype.GraphInfo, error)
	IsSystemDatabaseFunc func(databaseName string) bool
}

func ListGraphCatalogChildren(ctx context.Context, callbacks GraphCatalogCallbacks, engineID uint, connInfo ConnectionInfo, parent EngineCatalogPath, opts ListOptions) ([]EngineCatalogEntry, error) {
	model := GraphCatalogModel()
	if IsEngineCatalogRootPath(parent) {
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
		nodes := make([]EngineCatalogEntry, 0, len(namespaces))
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

	return []EngineCatalogEntry{{
		Name: EngineCatalogKindGraph,
		Path: appendCatalogSegment(parent, engineID, EngineCatalogTermGraph, EngineCatalogKindGraph, EngineCatalogKindGraph),
		Term: EngineCatalogTermGraph,
		Kind: EngineCatalogKindGraph,
		Role: EngineCatalogRoleLeaf,
	}}, nil
}

func (a GraphCatalogCallbacks) isSystemDatabase(databaseName string) bool {
	if a.IsSystemDatabaseFunc == nil {
		return false
	}
	return a.IsSystemDatabaseFunc(databaseName)
}

func ResolveGraphCatalogPath(ctx context.Context, callbacks GraphCatalogCallbacks, engineID uint, connInfo ConnectionInfo, path EngineCatalogPath) (*EngineCatalogEntry, error) {
	model := GraphCatalogModel()
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
	return &EngineCatalogEntry{Name: last.Name, Path: path, Term: EngineCatalogTermGraph, Kind: EngineCatalogKindGraph, Role: EngineCatalogRoleLeaf}, nil
}

func DescribeGraphCatalogFacts(ctx context.Context, callbacks GraphCatalogCallbacks, engineID uint, connInfo ConnectionInfo, path EngineCatalogPath, opts EngineCatalogFactsOptions) (*EngineCatalogFacts, error) {
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
	return &EngineCatalogFacts{
		Path:  path,
		Kind:  EngineCatalogKindGraph,
		Graph: graph.Clone(),
	}, nil
}
