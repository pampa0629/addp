package preview

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/addp/common/catalogview"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
	"github.com/addp/manager/internal/repository"
)

type namedPreviewProvider struct {
	name string
}

func (p namedPreviewProvider) Name() string { return p.name }
func (p namedPreviewProvider) Preview(context.Context, *PreviewRequest) (*models.TablePreview, error) {
	return nil, nil
}

func TestLoadPreviewPluginsRegistersBuiltinDefaultsWithoutFiles(t *testing.T) {
	registry := NewPreviewRegistry()
	repo := repository.NewMetadataRepository(nil, nil)
	LoadPreviewPlugins(registry, repo, nil, objectcontent.NewObjectContentRegistry(), "", "")

	for _, name := range []string{
		"builtin:database-table",
		"builtin:doc-collection",
		"builtin:graph-label",
		"builtin:graph-relationship",
		"builtin:scope-table",
		"builtin:container-child",
		"builtin:ref-file",
		"builtin:file-table",
		"builtin:object-catalog",
		"builtin:file-catalog",
		"builtin:schema-node",
	} {
		if _, err := registry.GetByName(name); err != nil {
			t.Fatalf("expected default provider %s: %v", name, err)
		}
	}
}

func TestLoadPreviewPluginsUsesManifestDefaultProviders(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.json")
	config := []byte(`{"default_providers":[{"name":"builtin:file-table","type":"builtin","builtin":"file-table"}]}`)
	if err := os.WriteFile(manifestPath, config, 0o600); err != nil {
		t.Fatalf("write provider manifest: %v", err)
	}

	registry := NewPreviewRegistry()
	repo := repository.NewMetadataRepository(nil, nil)
	LoadPreviewPlugins(registry, repo, nil, objectcontent.NewObjectContentRegistry(), "", manifestPath)

	if _, err := registry.GetByName("builtin:file-table"); err != nil {
		t.Fatalf("expected manifest default file-table: %v", err)
	}
	if _, err := registry.GetByName("builtin:file-catalog"); err == nil {
		t.Fatal("expected file-catalog to be absent because manifest defaults only contain file-table")
	}
}

func TestLoadPreviewPluginsCanDisableDefaultProvider(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.json")
	config := []byte(`{"providers":[{"name":"builtin:file-catalog","type":"builtin","builtin":"file-catalog","enabled":false}]}`)
	if err := os.WriteFile(manifestPath, config, 0o600); err != nil {
		t.Fatalf("write provider manifest: %v", err)
	}

	registry := NewPreviewRegistry()
	repo := repository.NewMetadataRepository(nil, nil)
	LoadPreviewPlugins(registry, repo, nil, objectcontent.NewObjectContentRegistry(), "", manifestPath)

	if _, err := registry.GetByName("builtin:file-catalog"); err == nil {
		t.Fatal("expected builtin:file-catalog to be disabled")
	}
	if _, err := registry.GetByName("builtin:file-table"); err != nil {
		t.Fatalf("expected other default providers to remain registered: %v", err)
	}
}

func TestResolveProviderByMetaUsesWholeTableLayout(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:scope-table"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &catalogview.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "table",
				"format":    "parquet",
				"layout":    "whole",
			},
		}},
		ItemType: "table",
	}
	provider, err := resolver.resolveProviderByMeta(req, &PreviewRequest{Engine: &models.Engine{EngineType: "minio"}, Table: "dataset"})
	if err != nil {
		t.Fatalf("resolveProviderByMeta() error = %v", err)
	}
	if provider.Name() != "builtin:scope-table" {
		t.Fatalf("provider = %q, want builtin:scope-table", provider.Name())
	}
}

func TestResolveProviderByMetaDoesNotRouteWholeTableWithoutScopeProvider(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:scope-table"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &catalogview.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "table",
				"format":    "orc",
				"layout":    "whole",
			},
		}},
		ItemType: "table",
	}
	if _, err := resolver.resolveProviderByMeta(req, &PreviewRequest{Engine: &models.Engine{EngineType: "minio"}, Table: "dataset"}); err == nil {
		t.Fatal("expected no provider when format has no scope table implementation")
	}
}

func TestResolveProviderByMetaUsesItemDataTypeAndFormat(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:file-table"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &catalogview.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "table",
				"format":    "json",
			},
		}},
		ItemType: "object",
	}
	provider, err := resolver.resolveProviderByMeta(req, &PreviewRequest{Engine: &models.Engine{EngineType: "minio"}, Table: "roads.geojson"})
	if err != nil {
		t.Fatalf("resolveProviderByMeta() error = %v", err)
	}
	if provider.Name() != "builtin:file-table" {
		t.Fatalf("provider = %q, want builtin:file-table", provider.Name())
	}
}

func TestResolveProviderByMetaUsesFileTableForFileCatalogTableFormat(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:file-table"})
	registry.Register(namedPreviewProvider{name: "builtin:file-catalog"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &catalogview.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "nfs"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "table",
				"format":    "csv",
				"layout":    "single",
			},
		}},
		ItemType: "file",
	}
	provider, err := resolver.resolveProviderByMeta(req, &PreviewRequest{Engine: &models.Engine{EngineType: "nfs"}, Table: "gis-data/sample.csv"})
	if err != nil {
		t.Fatalf("resolveProviderByMeta() error = %v", err)
	}
	if provider.Name() != "builtin:file-table" {
		t.Fatalf("provider = %q, want builtin:file-table", provider.Name())
	}
}

func TestResolveProviderByMetaUsesContainerChildForExcelChild(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:container-child"})
	registry.Register(namedPreviewProvider{name: "builtin:object-catalog"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &catalogview.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "container",
				"format":    "excel",
				"layout":    "single",
			},
		}},
		ItemType:  "object",
		ChildName: "Cities",
	}
	provider, err := resolver.resolveProviderByMeta(req, &PreviewRequest{Engine: &models.Engine{EngineType: "minio"}, Table: "test.xlsx", ChildName: "Cities"})
	if err != nil {
		t.Fatalf("resolveProviderByMeta() error = %v", err)
	}
	if provider.Name() != "builtin:container-child" {
		t.Fatalf("provider = %q, want builtin:container-child", provider.Name())
	}
}

func TestResolveProviderByMetaUsesContainerChildForSQLiteChild(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:container-child"})
	registry.Register(namedPreviewProvider{name: "builtin:object-catalog"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &catalogview.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "container",
				"format":    "sqlite",
				"layout":    "single",
			},
		}},
		ItemType:  "object",
		ChildName: "cities",
	}
	provider, err := resolver.resolveProviderByMeta(req, &PreviewRequest{Engine: &models.Engine{EngineType: "minio"}, Table: "sample.db", ChildName: "cities"})
	if err != nil {
		t.Fatalf("resolveProviderByMeta() error = %v", err)
	}
	if provider.Name() != "builtin:container-child" {
		t.Fatalf("provider = %q, want builtin:container-child", provider.Name())
	}
}

func TestResolveProviderByMetaUsesContainerChildForNestedContainerChild(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:container-child"})
	registry.Register(namedPreviewProvider{name: "builtin:file-table"})
	registry.Register(namedPreviewProvider{name: "builtin:file-catalog"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &catalogview.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "nfs"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "container",
				"format":    "zip",
				"layout":    "single",
			},
		}},
		ItemType:        "file",
		ChildName:       "inner.zip",
		NestedChildPath: "roads.shp",
	}
	provider, err := resolver.resolveProviderByMeta(req, &PreviewRequest{
		Engine:          &models.Engine{EngineType: "nfs"},
		Table:           "outer.zip",
		ChildName:       "inner.zip",
		NestedChildPath: "roads.shp",
	})
	if err != nil {
		t.Fatalf("resolveProviderByMeta() error = %v", err)
	}
	if provider.Name() != "builtin:container-child" {
		t.Fatalf("provider = %q, want builtin:container-child", provider.Name())
	}
}

func TestResolveProviderByMetaPrefersPartitionedItemAttributes(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:file-table"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &catalogview.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "table",
				"format":    "json",
			},
		}},
		ItemType: "object",
	}
	provider, err := resolver.resolveProviderByMeta(req, &PreviewRequest{Engine: &models.Engine{EngineType: "minio"}, Table: "roads.geojson"})
	if err != nil {
		t.Fatalf("resolveProviderByMeta() error = %v", err)
	}
	if provider.Name() != "builtin:file-table" {
		t.Fatalf("provider = %q, want builtin:file-table", provider.Name())
	}
}

func TestStringAttributeReadsPartitionedStorageOnly(t *testing.T) {
	attrs := map[string]interface{}{
		"physical_path": "legacy/path.geojson",
		"storage": map[string]interface{}{
			"physical_path": "bucket/path.geojson",
		},
	}

	if got := stringAttribute(attrs, "physical_path"); got != "bucket/path.geojson" {
		t.Fatalf("stringAttribute() = %q, want bucket/path.geojson", got)
	}

	if got := stringAttribute(map[string]interface{}{"physical_path": "legacy/path.geojson"}, "physical_path"); got != "" {
		t.Fatalf("stringAttribute() legacy flat = %q, want empty", got)
	}
}

func TestAttributeHelpersReadPartitionedSlicesAndNumbers(t *testing.T) {
	attrs := map[string]interface{}{
		"refs": []interface{}{
			map[string]interface{}{"path": "legacy/a.shp"},
		},
		"item": map[string]interface{}{
			"refs": []interface{}{
				map[string]interface{}{"path": "bucket/roads/roads.shp"},
				map[string]interface{}{"path": "bucket/roads/roads.dbf"},
			},
		},
		"storage": map[string]interface{}{
			"total_size": float64(42),
		},
	}

	refs := refRefsFromAttributes(attrs)
	if len(refs) != 2 || refs[0].Ref.Path != "bucket/roads/roads.shp" {
		t.Fatalf("refs = %#v, want partitioned refs", refs)
	}
	if got := int64Attribute(attrs, "total_size"); got != 42 {
		t.Fatalf("total_size = %d, want 42", got)
	}
	if got := refRefsFromAttributes(map[string]interface{}{"refs": []interface{}{map[string]interface{}{"path": "legacy/a.shp"}}}); len(got) != 0 {
		t.Fatalf("legacy flat refs = %#v, want empty", got)
	}
}

func TestConvertToLegacyRequestUsesPartitionedPhysicalPath(t *testing.T) {
	resolver := NewPreviewResolver(NewPreviewRegistry(), nil, nil)
	req := &PreviewResolverRequest{
		Locator: &catalogview.ResourceLocator{
			Path: []string{"bucket", "table.parquet"},
		},
		Engine:       &commonModels.Engine{EngineType: "minio"},
		Metadata:     &commonModels.MetaNode{Attributes: map[string]interface{}{}},
		ItemType:     "table",
		PhysicalPath: "bucket/table.parquet",
	}

	legacyReq := resolver.convertToLegacyRequest(req)
	if legacyReq.PhysicalPath != "bucket/table.parquet" {
		t.Fatalf("PhysicalPath = %q, want bucket/table.parquet", legacyReq.PhysicalPath)
	}
}

func TestConvertToLegacyRequestKeepsChildName(t *testing.T) {
	resolver := NewPreviewResolver(NewPreviewRegistry(), nil, nil)
	req := &PreviewResolverRequest{
		Locator: &catalogview.ResourceLocator{Path: []string{"bucket", "test.xlsx"}},
		Engine:  &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{"data_type": "container", "format": "excel"},
		}},
		ItemType:  "object",
		ChildName: "Cities",
	}

	legacyReq := resolver.convertToLegacyRequest(req)
	if legacyReq.ChildName != "Cities" {
		t.Fatalf("ChildName = %q, want Cities", legacyReq.ChildName)
	}
}

func TestConvertToLegacyRequestUsesScopePathForWholeScopeTable(t *testing.T) {
	req := &PreviewResolverRequest{
		Locator: &catalogview.ResourceLocator{
			Path: []string{"bucket", "dataset"},
		},
		Engine: &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"storage": map[string]interface{}{
				"physical_path": "bucket/dataset",
			},
			"item": map[string]interface{}{
				"layout": "whole",
			},
		}},
		ItemType: "table",
	}

	physicalPath, scopePath := previewResourcePaths(req.Metadata.Attributes)
	if physicalPath != "" {
		t.Fatalf("physicalPath = %q, want empty for whole scope table", physicalPath)
	}
	if scopePath != "bucket/dataset" {
		t.Fatalf("scopePath = %q, want bucket/dataset", scopePath)
	}
}

func TestResolveProviderByMetaRejectsUnmappedMeta(t *testing.T) {
	resolver := NewPreviewResolver(NewPreviewRegistry(), nil, nil)
	req := &PreviewResolverRequest{
		Locator:  &catalogview.ResourceLocator{},
		Engine:   &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{}},
		ItemType: "object",
	}

	if _, err := resolver.resolveProviderByMeta(req, &PreviewRequest{Engine: &models.Engine{EngineType: "minio"}, Table: "unknown.bin"}); err == nil {
		t.Fatal("expected error for unmapped meta")
	}
}
