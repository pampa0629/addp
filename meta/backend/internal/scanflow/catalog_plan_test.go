package scanflow

import (
	"context"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestCatalogScanPlanForPluginUsesCatalogModel(t *testing.T) {
	tests := []struct {
		name         string
		model        plugin.CatalogModelSpec
		wantStrategy CatalogScanStrategy
		wantBranch   string
		wantLeaf     string
	}{
		{
			name:         "tabular",
			model:        plugin.TabularCatalogModel(plugin.CatalogTermSchema),
			wantStrategy: CatalogScanTabular,
			wantBranch:   plugin.CatalogTermSchema,
			wantLeaf:     plugin.CatalogTermTable,
		},
		{
			name:         "dynamic schema",
			model:        plugin.DynamicSchemaCatalogModel(),
			wantStrategy: CatalogScanBranchLeaves,
			wantBranch:   plugin.CatalogTermDatabase,
			wantLeaf:     plugin.CatalogTermCollection,
		},
		{
			name: "direct leaf",
			model: plugin.CatalogModelSpec{
				PathVersion: plugin.CatalogPathVersion,
				RootTerm:    plugin.CatalogTermService,
				Levels: []plugin.CatalogLevelSpec{
					{Term: "topic", Kinds: []string{"topic"}, Role: plugin.CatalogRoleLeaf},
				},
			},
			wantStrategy: CatalogScanDirectLeaves,
			wantLeaf:     "topic",
		},
		{
			name:         "object",
			model:        plugin.ObjectCatalogModel(),
			wantStrategy: CatalogScanObject,
			wantLeaf:     plugin.CatalogTermObject,
		},
		{
			name:         "file",
			model:        plugin.FileCatalogModel(),
			wantStrategy: CatalogScanFile,
			wantLeaf:     plugin.CatalogTermFile,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan, ok := CatalogScanPlanForPlugin(catalogPlanTestPlugin{model: tt.model})
			if !ok {
				t.Fatal("CatalogScanPlanForPlugin() returned false")
			}
			if plan.Strategy != tt.wantStrategy {
				t.Fatalf("strategy = %s, want %s", plan.Strategy, tt.wantStrategy)
			}
			if plan.BranchTerm != tt.wantBranch {
				t.Fatalf("branchTerm = %s, want %s", plan.BranchTerm, tt.wantBranch)
			}
			if plan.LeafTerm != tt.wantLeaf {
				t.Fatalf("leafTerm = %s, want %s", plan.LeafTerm, tt.wantLeaf)
			}
		})
	}
}

type catalogPlanTestPlugin struct {
	model plugin.CatalogModelSpec
}

func (p catalogPlanTestPlugin) Type() string         { return "scan-strategy-test" }
func (p catalogPlanTestPlugin) DisplayName() string  { return "Scan Strategy Test" }
func (p catalogPlanTestPlugin) EngineOrigin() string { return "general" }
func (p catalogPlanTestPlugin) DefaultPort() int     { return 0 }
func (p catalogPlanTestPlugin) RequiredFields() []string {
	return nil
}
func (p catalogPlanTestPlugin) SensitiveFields() []string {
	return nil
}
func (p catalogPlanTestPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p catalogPlanTestPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p catalogPlanTestPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (p catalogPlanTestPlugin) CatalogModel() plugin.CatalogModelSpec {
	return p.model
}

var _ plugin.EnginePlugin = catalogPlanTestPlugin{}
var _ plugin.CatalogModelProvider = catalogPlanTestPlugin{}
