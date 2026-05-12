package service

import (
	"context"
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resource"
	"github.com/addp/manager/internal/models"
)

type namedPreviewProvider struct {
	name string
}

func (p namedPreviewProvider) Name() string { return p.name }
func (p namedPreviewProvider) Preview(context.Context, *PreviewRequest) (*models.TablePreview, error) {
	return nil, nil
}

func TestResolveProviderByMetaUsesWholeTableOrganization(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:scope-table"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &resource.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type":    "table",
				"format":       "parquet",
				"organization": "whole",
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
		Locator: &resource.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type":    "table",
				"format":       "orc",
				"organization": "whole",
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
		Locator: &resource.ResourceLocator{},
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

func TestResolveProviderByMetaUsesFileTableForFileSystemTableFormat(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:file-table"})
	registry.Register(namedPreviewProvider{name: "builtin:filesystem"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &resource.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "nfs"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type":    "table",
				"format":       "csv",
				"organization": "single",
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

func TestResolveProviderByMetaPrefersPartitionedItemAttributes(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:file-table"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &resource.ResourceLocator{},
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
		"component_files": []interface{}{"legacy/a.shp"},
		"item": map[string]interface{}{
			"component_files": []interface{}{"bucket/roads/roads.shp", "bucket/roads/roads.dbf"},
		},
		"storage": map[string]interface{}{
			"total_size": float64(42),
		},
	}

	files := stringSliceAttribute(attrs, "component_files")
	if len(files) != 2 || files[0] != "bucket/roads/roads.shp" {
		t.Fatalf("component_files = %#v, want partitioned files", files)
	}
	if got := int64Attribute(attrs, "total_size"); got != 42 {
		t.Fatalf("total_size = %d, want 42", got)
	}
	if got := stringSliceAttribute(map[string]interface{}{"component_files": []interface{}{"legacy/a.shp"}}, "component_files"); len(got) != 0 {
		t.Fatalf("legacy flat component_files = %#v, want empty", got)
	}
}

func TestConvertToLegacyRequestUsesPartitionedPhysicalPath(t *testing.T) {
	resolver := NewPreviewResolver(NewPreviewRegistry(), nil, nil)
	req := &PreviewResolverRequest{
		Locator: &resource.ResourceLocator{
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

func TestConvertToLegacyRequestUsesScopePathForWholeScopeTable(t *testing.T) {
	req := &PreviewResolverRequest{
		Locator: &resource.ResourceLocator{
			Path: []string{"bucket", "dataset"},
		},
		Engine: &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"storage": map[string]interface{}{
				"physical_path": "bucket/dataset",
			},
			"item": map[string]interface{}{
				"organization": "whole",
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
		Locator:  &resource.ResourceLocator{},
		Engine:   &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{}},
		ItemType: "object",
	}

	if _, err := resolver.resolveProviderByMeta(req, &PreviewRequest{Engine: &models.Engine{EngineType: "minio"}, Table: "unknown.bin"}); err == nil {
		t.Fatal("expected error for unmapped meta")
	}
}
