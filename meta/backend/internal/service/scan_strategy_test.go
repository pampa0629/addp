package service

import (
	"context"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestCatalogScanPlanForPluginUsesCatalogModel(t *testing.T) {
	tests := []struct {
		name         string
		model        plugin.CatalogModelSpec
		wantStrategy catalogScanStrategy
		wantBranch   string
		wantLeaf     string
	}{
		{
			name:         "tabular",
			model:        plugin.TabularCatalogModel(plugin.CatalogTermSchema),
			wantStrategy: catalogScanTabular,
			wantBranch:   plugin.CatalogTermSchema,
			wantLeaf:     plugin.CatalogTermTable,
		},
		{
			name:         "dynamic schema",
			model:        plugin.DynamicSchemaCatalogModel(),
			wantStrategy: catalogScanBranchLeaves,
			wantBranch:   plugin.CatalogTermDatabase,
			wantLeaf:     plugin.CatalogTermCollection,
		},
		{
			name:         "object",
			model:        plugin.ObjectCatalogModel(),
			wantStrategy: catalogScanObject,
			wantLeaf:     plugin.CatalogTermObject,
		},
		{
			name:         "file",
			model:        plugin.FileCatalogModel(),
			wantStrategy: catalogScanFile,
			wantLeaf:     plugin.CatalogTermFile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, ok := catalogScanPlanForPlugin(scanStrategyTestPlugin{model: tt.model})
			if !ok {
				t.Fatal("catalogScanPlanForPlugin() returned false")
			}
			if plan.strategy != tt.wantStrategy {
				t.Fatalf("strategy = %s, want %s", plan.strategy, tt.wantStrategy)
			}
			if plan.branchTerm != tt.wantBranch {
				t.Fatalf("branchTerm = %s, want %s", plan.branchTerm, tt.wantBranch)
			}
			if plan.leafTerm != tt.wantLeaf {
				t.Fatalf("leafTerm = %s, want %s", plan.leafTerm, tt.wantLeaf)
			}
		})
	}
}

type scanStrategyTestPlugin struct {
	model plugin.CatalogModelSpec
}

func (p scanStrategyTestPlugin) Type() string         { return "scan-strategy-test" }
func (p scanStrategyTestPlugin) DisplayName() string  { return "Scan Strategy Test" }
func (p scanStrategyTestPlugin) EngineOrigin() string { return "general" }
func (p scanStrategyTestPlugin) DefaultPort() int     { return 0 }
func (p scanStrategyTestPlugin) RequiredFields() []string {
	return nil
}
func (p scanStrategyTestPlugin) SensitiveFields() []string {
	return nil
}
func (p scanStrategyTestPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p scanStrategyTestPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p scanStrategyTestPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (p scanStrategyTestPlugin) CatalogModel() plugin.CatalogModelSpec {
	return p.model
}

var _ plugin.EnginePlugin = scanStrategyTestPlugin{}
var _ plugin.CatalogModelProvider = scanStrategyTestPlugin{}
