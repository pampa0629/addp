package nfs

import (
	"context"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestNFSRootCatalogNodeIsDot(t *testing.T) {
	p := &NFSPlugin{}
	callbacks := plugin.FileCatalogCallbacks{
		ListRootsFunc: func(ctx context.Context, connInfo plugin.ConnectionInfo) ([]plugin.RootEntry, error) {
			return []plugin.RootEntry{{Name: ".", Path: "/"}}, nil
		},
	}

	nodes, err := plugin.ListFileCatalogChildren(context.Background(), callbacks, nil, 7, plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: 7,
	}, plugin.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected one root node, got %d", len(nodes))
	}
	root := nodes[0]
	if root.Name != "." {
		t.Fatalf("expected root name '.', got %q", root.Name)
	}
	if root.Term != plugin.CatalogTermRoot || root.Kind != plugin.CatalogKindRoot {
		t.Fatalf("expected root term/kind, got %s/%s", root.Term, root.Kind)
	}
	if got := p.CatalogModel().RootTerm; got != plugin.CatalogTermRoot {
		t.Fatalf("expected file catalog root term %q, got %q", plugin.CatalogTermRoot, got)
	}
}
