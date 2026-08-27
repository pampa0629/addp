package scanflow

import (
	"context"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestEngineCatalogScanPlanForPluginUsesEngineCatalogModel(t *testing.T) {
	tests := []struct {
		name         string
		model        plugin.EngineCatalogModelSpec
		wantStrategy EngineCatalogScanStrategy
		wantBranch   string
		wantLeaf     string
	}{
		{
			name:         "tabular",
			model:        plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema),
			wantStrategy: EngineCatalogScanTabular,
			wantBranch:   plugin.EngineCatalogTermSchema,
			wantLeaf:     plugin.EngineCatalogTermTable,
		},
		{
			name:         "dynamic schema",
			model:        plugin.DynamicSchemaCatalogModel(),
			wantStrategy: EngineCatalogScanBranchLeaves,
			wantBranch:   plugin.EngineCatalogTermDatabase,
			wantLeaf:     plugin.EngineCatalogTermCollection,
		},
		{
			name: "direct leaf",
			model: plugin.EngineCatalogModelSpec{
				PathVersion: plugin.EngineCatalogPathVersion,
				RootTerm:    plugin.EngineCatalogTermService,
				Levels: []plugin.EngineCatalogLevelSpec{
					{Term: "topic", Kinds: []string{"topic"}, Role: plugin.EngineCatalogRoleLeaf},
				},
			},
			wantStrategy: EngineCatalogScanDirectLeaves,
			wantLeaf:     "topic",
		},
		{
			name:         "object",
			model:        plugin.ObjectCatalogModel(),
			wantStrategy: EngineCatalogScanObject,
			wantLeaf:     plugin.EngineCatalogTermObject,
		},
		{
			name:         "file",
			model:        plugin.FileCatalogModel(),
			wantStrategy: EngineCatalogScanFile,
			wantLeaf:     plugin.EngineCatalogTermFile,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan, ok := EngineCatalogScanPlanForPlugin(engineCatalogPlanTestPlugin{model: tt.model})
			if !ok {
				t.Fatal("EngineCatalogScanPlanForPlugin() returned false")
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

type engineCatalogPlanTestPlugin struct {
	model plugin.EngineCatalogModelSpec
}

func (p engineCatalogPlanTestPlugin) Type() string         { return "scan-strategy-test" }
func (p engineCatalogPlanTestPlugin) DisplayName() string  { return "Scan Strategy Test" }
func (p engineCatalogPlanTestPlugin) EngineOrigin() string { return "general" }
func (p engineCatalogPlanTestPlugin) DefaultPort() int     { return 0 }
func (p engineCatalogPlanTestPlugin) RequiredFields() []string {
	return nil
}
func (p engineCatalogPlanTestPlugin) SensitiveFields() []string {
	return nil
}
func (p engineCatalogPlanTestPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p engineCatalogPlanTestPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p engineCatalogPlanTestPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (p engineCatalogPlanTestPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return p.model
}

var _ plugin.EnginePlugin = engineCatalogPlanTestPlugin{}
var _ plugin.EngineCatalogModelProvider = engineCatalogPlanTestPlugin{}
