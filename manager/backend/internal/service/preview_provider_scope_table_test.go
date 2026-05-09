package service

import (
	"context"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resource"
	"github.com/addp/manager/internal/models"
)

type recordingCatalogProvider struct {
	parent plugin.CatalogPath
}

func (p *recordingCatalogProvider) Type() string         { return "recording" }
func (p *recordingCatalogProvider) DisplayName() string  { return "recording" }
func (p *recordingCatalogProvider) Description() string  { return "" }
func (p *recordingCatalogProvider) Version() string      { return "" }
func (p *recordingCatalogProvider) EngineOrigin() string { return "general" }
func (p *recordingCatalogProvider) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingCatalogProvider) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingCatalogProvider) DefaultPort() int          { return 0 }
func (p *recordingCatalogProvider) RequiredFields() []string  { return nil }
func (p *recordingCatalogProvider) SensitiveFields() []string { return nil }
func (p *recordingCatalogProvider) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (p *recordingCatalogProvider) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (p *recordingCatalogProvider) ListChildren(_ context.Context, _ plugin.ConnectionInfo, parent plugin.CatalogPath, _ plugin.ListOptions) ([]plugin.CatalogNode, error) {
	p.parent = parent
	return nil, nil
}
func (p *recordingCatalogProvider) ResolvePath(context.Context, plugin.ConnectionInfo, plugin.CatalogPath) (*plugin.CatalogNode, error) {
	return nil, nil
}

func TestScopeTableResourceReaderUsesObjectStorageReader(t *testing.T) {
	t.Parallel()

	reader, err := scopeTableResourceReader(&PreviewRequest{
		Engine: &models.Engine{EngineType: "minio", ID: 7},
		Schema: "demo",
	}, nil, nil, plugin.ConnectionInfo{"bucket": "demo"})
	if err != nil {
		t.Fatalf("scopeTableResourceReader() error = %v", err)
	}
	if _, ok := reader.(*objectStorageResourceReader); !ok {
		t.Fatalf("reader = %T, want *objectStorageResourceReader", reader)
	}
}

func TestScopeTableResourceReaderUsesFileSystemReader(t *testing.T) {
	t.Parallel()

	reader, err := scopeTableResourceReader(&PreviewRequest{
		Engine: &models.Engine{EngineType: "nfs", ID: 7},
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("scopeTableResourceReader() error = %v", err)
	}
	if _, ok := reader.(*fileSystemResourceReader); !ok {
		t.Fatalf("reader = %T, want *fileSystemResourceReader", reader)
	}
}

func TestObjectStorageResourceReaderListTrimsBucketFromScope(t *testing.T) {
	t.Parallel()

	catalog := &recordingCatalogProvider{}
	reader := newObjectStorageResourceReader(nil, catalog, nil, 7, "demo")
	if _, err := reader.List(context.Background(), resource.NewResourceRef("demo/dataset", resource.ResourceRoleScope)); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if got := catalog.parent.StringPath(); got != "demo/dataset" {
		t.Fatalf("catalog path = %q, want demo/dataset", got)
	}
}
