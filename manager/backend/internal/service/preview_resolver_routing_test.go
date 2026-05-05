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

func (p namedPreviewProvider) Name() string                  { return p.name }
func (p namedPreviewProvider) Priority() int                 { return 0 }
func (p namedPreviewProvider) Supports(*PreviewRequest) bool { return false }
func (p namedPreviewProvider) Preview(context.Context, *PreviewRequest) (*models.TablePreview, error) {
	return nil, nil
}

func TestResolveProviderByMetaUsesItemType(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:lake-table"})
	resolver := NewPreviewResolver(registry, nil, nil, nil)

	req := &PreviewResolverRequest{
		Locator:  &resource.ResourceLocator{},
		Engine:   &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{}},
		ItemType: "lake_table",
	}
	provider, err := resolver.resolveProviderByMeta(req, &PreviewRequest{Engine: &models.Engine{EngineType: "minio"}, Table: "dataset"})
	if err != nil {
		t.Fatalf("resolveProviderByMeta() error = %v", err)
	}
	if provider.Name() != "builtin:lake-table" {
		t.Fatalf("provider = %q, want builtin:lake-table", provider.Name())
	}
}

func TestResolveProviderByMetaUsesDataFamilyAndFormat(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:file-table"})
	resolver := NewPreviewResolver(registry, nil, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &resource.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"data_family": "tabular",
			"format":      "geojson",
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

func TestResolveProviderByMetaPrefersPartitionedItemAttributes(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:file-table"})
	resolver := NewPreviewResolver(registry, nil, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &resource.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"data_family": "document",
			"format":      "pdf",
			"item": map[string]interface{}{
				"data_family": "tabular",
				"format":      "geojson",
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

func TestStringAttributeReadsPartitionedStorageBeforeFlatFallback(t *testing.T) {
	attrs := map[string]interface{}{
		"physical_path": "legacy/path.geojson",
		"storage": map[string]interface{}{
			"physical_path": "bucket/path.geojson",
		},
	}

	if got := stringAttribute(attrs, "physical_path"); got != "bucket/path.geojson" {
		t.Fatalf("stringAttribute() = %q, want bucket/path.geojson", got)
	}
}

func TestConvertToLegacyRequestUsesPartitionedPhysicalPath(t *testing.T) {
	resolver := NewPreviewResolver(NewPreviewRegistry(), nil, nil, nil)
	req := &PreviewResolverRequest{
		Locator: &resource.ResourceLocator{
			Path: []string{"bucket", "table.parquet"},
		},
		Engine:       &commonModels.Engine{EngineType: "minio"},
		Metadata:     &commonModels.MetaNode{Attributes: map[string]interface{}{}},
		ItemType:     "lake_table",
		PhysicalPath: "bucket/table.parquet",
	}

	legacyReq := resolver.convertToLegacyRequest(req)
	if legacyReq.PhysicalPath != "bucket/table.parquet" {
		t.Fatalf("PhysicalPath = %q, want bucket/table.parquet", legacyReq.PhysicalPath)
	}
}

func TestResolveProviderByMetaRejectsUnmappedMeta(t *testing.T) {
	resolver := NewPreviewResolver(NewPreviewRegistry(), nil, nil, nil)
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
