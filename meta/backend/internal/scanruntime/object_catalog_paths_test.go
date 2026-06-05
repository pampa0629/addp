package scanruntime

import (
	"context"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
)

func TestResolveObjectCatalogTargetDistinguishesObjectAndPrefix(t *testing.T) {
	t.Parallel()

	sizeBytes := int64(100)
	provider := objectScanTargetProvider{
		items: map[string]plugin.CatalogEntry{
			"addp/contain/shapefile.zip": {
				Name: "shapefile.zip",
				Path: plugin.ObjectItemPath(9, "addp", "contain/shapefile.zip"),
				Kind: plugin.CatalogKindObject,
				Term: plugin.CatalogTermObject,
				Role: plugin.CatalogRoleLeaf,
				Storage: &plugin.CatalogStorageFacts{
					Path:      "addp/contain/shapefile.zip",
					SizeBytes: &sizeBytes,
				},
			},
		},
	}
	resource := &commonModels.Engine{ID: 9, EngineType: provider.Type()}

	objectTarget, err := resolveObjectCatalogTarget(context.Background(), resource, provider, "addp/contain/shapefile.zip")
	if err != nil {
		t.Fatalf("resolve object target: %v", err)
	}
	if objectTarget.Bucket != "addp" || objectTarget.Object != "contain/shapefile.zip" || objectTarget.Prefix != "" {
		t.Fatalf("object target = %#v, want exact object", objectTarget)
	}

	prefixTarget, err := resolveObjectCatalogTarget(context.Background(), resource, provider, "addp/contain")
	if err != nil {
		t.Fatalf("resolve prefix target: %v", err)
	}
	if prefixTarget.Bucket != "addp" || prefixTarget.Prefix != "contain" || prefixTarget.Object != "" {
		t.Fatalf("prefix target = %#v, want prefix", prefixTarget)
	}
}

type objectScanTargetProvider struct {
	items map[string]plugin.CatalogEntry
}

func (p objectScanTargetProvider) Type() string         { return "object-scan-target-test" }
func (p objectScanTargetProvider) DisplayName() string  { return "object scan target test" }
func (p objectScanTargetProvider) EngineOrigin() string { return "general" }
func (p objectScanTargetProvider) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p objectScanTargetProvider) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (p objectScanTargetProvider) DefaultPort() int                                   { return 0 }
func (p objectScanTargetProvider) RequiredFields() []string                           { return nil }
func (p objectScanTargetProvider) SensitiveFields() []string                          { return nil }
func (p objectScanTargetProvider) Capabilities() plugin.EngineCapabilities {
	return plugin.NewObjectCapabilities(p.Type())
}
func (p objectScanTargetProvider) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (p objectScanTargetProvider) CatalogModel() plugin.CatalogModelSpec {
	return plugin.ObjectCatalogModel()
}
func (p objectScanTargetProvider) ListChildren(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	return nil, nil
}
func (p objectScanTargetProvider) ResolvePath(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogEntry, error) {
	node, ok := p.items[path.StringPath()]
	if !ok {
		return nil, nil
	}
	return &node, nil
}
