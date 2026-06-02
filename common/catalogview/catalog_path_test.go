package catalogview

import (
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestProviderCatalogPathFromLocatorTabularTable(t *testing.T) {
	t.Parallel()

	got, err := ProviderCatalogPathFromLocator(plugin.TabularCatalogModel(plugin.CatalogTermSchema), &ResourceLocator{
		EngineID: 7,
		Path:     []string{"public", "users"},
		Type:     TypeTable,
	})
	if err != nil {
		t.Fatalf("ProviderCatalogPathFromLocator() error = %v", err)
	}
	want := plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: 7,
		Segments: []plugin.CatalogSegment{
			{Term: plugin.CatalogTermServer, Kind: plugin.CatalogTermServer},
			{Term: plugin.CatalogTermSchema, Kind: plugin.CatalogKindNamespace, Name: "public"},
			{Term: plugin.CatalogTermTable, Kind: plugin.CatalogKindTable, Name: "users"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("path = %#v, want %#v", got, want)
	}
}

func TestProviderCatalogPathFromLocatorDynamicCollection(t *testing.T) {
	t.Parallel()

	got, err := ProviderCatalogPathFromLocator(plugin.DynamicSchemaCatalogModel(), &ResourceLocator{
		EngineID: 8,
		Path:     []string{"business", "orders"},
		Type:     TypeCollection,
	})
	if err != nil {
		t.Fatalf("ProviderCatalogPathFromLocator() error = %v", err)
	}
	assertCatalogSegments(t, got, []plugin.CatalogSegment{
		{Term: plugin.CatalogTermServer, Kind: plugin.CatalogTermServer},
		{Term: plugin.CatalogTermDatabase, Kind: plugin.CatalogKindNamespace, Name: "business"},
		{Term: plugin.CatalogTermCollection, Kind: plugin.CatalogKindCollection, Name: "orders"},
	})
}

func TestProviderCatalogPathFromLocatorGraph(t *testing.T) {
	t.Parallel()

	got, err := ProviderCatalogPathFromLocator(plugin.GraphCatalogModel(), &ResourceLocator{
		EngineID: 9,
		Path:     []string{"neo4j", "graph"},
		Type:     TypeGraph,
	})
	if err != nil {
		t.Fatalf("ProviderCatalogPathFromLocator() error = %v", err)
	}
	assertCatalogSegments(t, got, []plugin.CatalogSegment{
		{Term: plugin.CatalogTermServer, Kind: plugin.CatalogTermServer},
		{Term: plugin.CatalogTermDatabase, Kind: plugin.CatalogKindNamespace, Name: "neo4j"},
		{Term: plugin.CatalogTermGraph, Kind: plugin.CatalogKindGraph, Name: "graph"},
	})
}

func TestProviderCatalogPathFromLocatorObject(t *testing.T) {
	t.Parallel()

	got, err := ProviderCatalogPathFromLocator(plugin.ObjectCatalogModel(), &ResourceLocator{
		EngineID: 10,
		Path:     []string{"bucket", "a", "b.csv"},
		Type:     TypeObject,
	})
	if err != nil {
		t.Fatalf("ProviderCatalogPathFromLocator() error = %v", err)
	}
	assertCatalogSegments(t, got, []plugin.CatalogSegment{
		{Term: plugin.CatalogTermService, Kind: plugin.CatalogTermService},
		{Term: plugin.CatalogTermBucket, Kind: plugin.CatalogKindBucket, Name: "bucket"},
		{Term: plugin.CatalogTermPrefix, Kind: plugin.CatalogKindPrefix, Name: "a"},
		{Term: plugin.CatalogTermObject, Kind: plugin.CatalogKindObject, Name: "b.csv"},
	})
}

func TestProviderCatalogPathFromLocatorFile(t *testing.T) {
	t.Parallel()

	got, err := ProviderCatalogPathFromLocator(plugin.FileCatalogModel(), &ResourceLocator{
		EngineID: 11,
		Path:     []string{"data", "a.csv"},
		Type:     TypeFile,
	})
	if err != nil {
		t.Fatalf("ProviderCatalogPathFromLocator() error = %v", err)
	}
	assertCatalogSegments(t, got, []plugin.CatalogSegment{
		{Term: plugin.CatalogTermRoot, Kind: plugin.CatalogTermRoot},
		{Term: plugin.CatalogTermDirectory, Kind: plugin.CatalogKindDirectory, Name: "data"},
		{Term: plugin.CatalogTermFile, Kind: plugin.CatalogKindFile, Name: "a.csv"},
	})
}

func TestProviderCatalogPathFromLocatorRoot(t *testing.T) {
	t.Parallel()

	got, err := ProviderCatalogPathFromLocator(plugin.ObjectCatalogModel(), &ResourceLocator{
		EngineID: 12,
		Type:     TypeRoot,
	})
	if err != nil {
		t.Fatalf("ProviderCatalogPathFromLocator() error = %v", err)
	}
	assertCatalogSegments(t, got, []plugin.CatalogSegment{
		{Term: plugin.CatalogTermService, Kind: plugin.CatalogTermService},
	})
}

func TestProviderCatalogPathFromLocatorRejectsMissingLeaf(t *testing.T) {
	t.Parallel()

	_, err := ProviderCatalogPathFromLocator(plugin.TabularCatalogModel(plugin.CatalogTermSchema), &ResourceLocator{
		EngineID: 7,
		Path:     []string{"public"},
		Type:     TypeTable,
	})
	if err == nil {
		t.Fatalf("expected missing leaf error")
	}
}

func assertCatalogSegments(t *testing.T, path plugin.CatalogPath, want []plugin.CatalogSegment) {
	t.Helper()
	if path.Version != plugin.CatalogPathVersion {
		t.Fatalf("path version = %q", path.Version)
	}
	if !reflect.DeepEqual(path.Segments, want) {
		t.Fatalf("segments = %#v, want %#v", path.Segments, want)
	}
}
