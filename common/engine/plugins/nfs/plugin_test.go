package nfs

import (
	"context"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestNFSRootCatalogNodeIsSemanticRoot(t *testing.T) {
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
	if root.Name != "/" {
		t.Fatalf("expected root name '/', got %q", root.Name)
	}
	if got := root.Path.StringPath(); got != "" {
		t.Fatalf("expected root string path '', got %q", got)
	}
	if got := root.Attributes["path"]; got != "" {
		t.Fatalf("expected root storage path '', got %#v", got)
	}
	if root.Term != plugin.CatalogTermRoot || root.Kind != plugin.CatalogKindRoot {
		t.Fatalf("expected root term/kind, got %s/%s", root.Term, root.Kind)
	}
	if got := p.CatalogModel().RootTerm; got != plugin.CatalogTermRoot {
		t.Fatalf("expected file catalog root term %q, got %q", plugin.CatalogTermRoot, got)
	}
}

func TestFileCatalogNormalizesDotRoot(t *testing.T) {
	callbacks := plugin.FileCatalogCallbacks{
		ListRootsFunc: func(ctx context.Context, connInfo plugin.ConnectionInfo) ([]plugin.RootEntry, error) {
			return []plugin.RootEntry{{Name: ".", Path: "/"}}, nil
		},
		ListDirectoryFunc: func(ctx context.Context, connInfo plugin.ConnectionInfo, path string) ([]plugin.FileEntry, []plugin.DirEntry, error) {
			if path != "" {
				t.Fatalf("expected semantic root list path '', got %q", path)
			}
			return nil, []plugin.DirEntry{{Name: "shp", Path: "./shp/"}}, nil
		},
	}

	roots, err := plugin.ListFileCatalogChildren(context.Background(), callbacks, nil, 7, plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: 7,
	}, plugin.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	children, err := plugin.ListFileCatalogChildren(context.Background(), callbacks, nil, 7, roots[0].Path, plugin.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 {
		t.Fatalf("expected one child, got %d", len(children))
	}
	if got := children[0].Path.StringPath(); got != "shp" {
		t.Fatalf("expected child path 'shp', got %q", got)
	}
	if got := children[0].Attributes["path"]; got != "shp" {
		t.Fatalf("expected child storage path 'shp', got %#v", got)
	}
}

func TestNFSCapabilitiesDeclareRangeRead(t *testing.T) {
	p := &NFSPlugin{}

	if _, ok := interface{}(p).(plugin.RangeReadableProvider); !ok {
		t.Fatal("NFSPlugin should implement RangeReadableProvider")
	}
	caps := p.Capabilities()
	if caps.Storage == nil || caps.Storage.Store == nil || !caps.Storage.Store.RangeRead {
		t.Fatalf("NFS range_read capability = %#v, want true", caps.Storage)
	}
	if err := plugin.ValidatePluginCapabilities(p); err != nil {
		t.Fatalf("ValidatePluginCapabilities() error = %v", err)
	}
}
