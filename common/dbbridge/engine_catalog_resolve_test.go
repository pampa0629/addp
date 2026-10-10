package dbbridge

import (
	"context"
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

type resolveOnlyCatalogProvider struct {
	sampleCatalogProvider
	calls int
	path  plugin.EngineCatalogPath
}

func (*resolveOnlyCatalogProvider) Type() string { return "test_exact_resolve" }
func (p *resolveOnlyCatalogProvider) ResolvePath(_ context.Context, _ plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	p.calls++
	p.path = path
	return &plugin.EngineCatalogEntry{Path: path, Kind: "collection", Term: "collection", Role: "leaf"}, nil
}
func (*resolveOnlyCatalogProvider) ListChildren(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	panic("exact resolution must not enumerate")
}

func TestResolveEngineCatalogPathUsesOnlyProviderResolution(t *testing.T) {
	p := &resolveOnlyCatalogProvider{}
	plugin.Register(p)
	path := plugin.EngineCatalogPath{Version: plugin.EngineCatalogPathVersion, EngineID: 9, Segments: []plugin.EngineCatalogSegment{{Term: "server", Kind: "server"}, {Term: "database", Kind: "namespace", Name: "Outdoor"}, {Term: "collection", Kind: "collection", Name: "Persons"}}}
	entry, err := ResolveEngineCatalogPath(context.Background(), &models.Engine{ID: 9, EngineType: p.Type()}, path)
	if err != nil || entry == nil || p.calls != 1 || !reflect.DeepEqual(p.path, path) || !reflect.DeepEqual(entry.Path, path) {
		t.Fatalf("entry=%+v err=%v calls=%d path=%+v", entry, err, p.calls, p.path)
	}
}
