package service

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
)

func TestNamespaceItemType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		node plugin.CatalogEntry
		want string
	}{
		{
			name: "dynamic schema collection",
			node: plugin.CatalogEntry{Term: plugin.CatalogTermCollection, Kind: plugin.CatalogKindCollection, Role: plugin.CatalogRoleLeaf},
			want: "collection",
		},
		{
			name: "graph",
			node: plugin.CatalogEntry{Term: plugin.CatalogTermGraph, Kind: plugin.CatalogKindGraph, Role: plugin.CatalogRoleLeaf},
			want: "graph",
		},
		{
			name: "container is not item",
			node: plugin.CatalogEntry{Term: plugin.CatalogTermDatabase, Kind: plugin.CatalogKindNamespace, Role: plugin.CatalogRoleBranch},
			want: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := namespaceLeafItemType(tt.node); got != tt.want {
				t.Fatalf("namespaceLeafItemType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFinalizeCatalogRootAfterNamespaceScan(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	svc := &ScanService{
		db:   db,
		repo: repo,
		log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	resource := &commonModels.Engine{ID: 25, Name: "Business Neo4j", EngineType: "namespace-root-test"}
	plugin.Register(namespaceRootTestPlugin{})
	t.Cleanup(func() {
		plugin.Unregister("namespace-root-test")
	})

	root, err := ensureCatalogRootNode(repo, 1, resource, namespaceRootTestPlugin{})
	if err != nil {
		t.Fatalf("ensureCatalogRootNode() error = %v", err)
	}
	if root.ScanStatus == "completed" {
		t.Fatal("test setup unexpectedly completed root node")
	}

	svc.finalizeCatalogRootAfterScan(resource, 1, 1, models.ScannedDepthDeep)

	var got models.MetaNode
	if err := db.Where("id = ?", root.ID).First(&got).Error; err != nil {
		t.Fatalf("query root node: %v", err)
	}
	if got.ScanStatus != "completed" || got.ScannedDepth != models.ScannedDepthDeep {
		t.Fatalf("root scan status/depth = %q/%q, want completed/deep", got.ScanStatus, got.ScannedDepth)
	}
	if got.ItemCount != 1 {
		t.Fatalf("root item_count = %d, want 1", got.ItemCount)
	}
}

type namespaceRootTestPlugin struct{}

func (namespaceRootTestPlugin) Type() string         { return "namespace-root-test" }
func (namespaceRootTestPlugin) DisplayName() string  { return "Namespace Root Test" }
func (namespaceRootTestPlugin) EngineOrigin() string { return "general" }
func (namespaceRootTestPlugin) DefaultPort() int     { return 0 }
func (namespaceRootTestPlugin) RequiredFields() []string {
	return nil
}
func (namespaceRootTestPlugin) SensitiveFields() []string {
	return nil
}
func (namespaceRootTestPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (namespaceRootTestPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (namespaceRootTestPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (namespaceRootTestPlugin) CatalogModel() plugin.CatalogModelSpec {
	return plugin.GraphCatalogModel()
}
func (namespaceRootTestPlugin) ListChildren(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	return nil, nil
}
func (namespaceRootTestPlugin) ResolvePath(context.Context, plugin.ConnectionInfo, plugin.CatalogPath) (*plugin.CatalogEntry, error) {
	return nil, nil
}
func (namespaceRootTestPlugin) DescribeCatalogFacts(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	return &plugin.CatalogFacts{Graph: &datatype.GraphInfo{}}, nil
}

var _ plugin.EnginePlugin = namespaceRootTestPlugin{}
var _ plugin.CatalogModelProvider = namespaceRootTestPlugin{}
var _ plugin.CatalogProvider = namespaceRootTestPlugin{}
var _ plugin.CatalogFactsProvider = namespaceRootTestPlugin{}
