package dbbridge

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

// PrepareTableResult adapts ADDP identity to the provider's frozen write plan.
// It neither binds SQL parameters nor executes SQL; the provider owns both.
func PrepareTableResult(ctx context.Context, engine *models.Engine, target *resourcetree.ResourceLocator, query string, parameters map[string]interface{}, mode string) (plugin.PreparedTableResult, error) {
	if engine == nil || target == nil || target.EngineID != engine.ID || target.Type != resourcetree.TypeTable {
		return nil, fmt.Errorf("invalid table result target")
	}
	provider, err := plugin.Get(engine.EngineType)
	if err != nil {
		return nil, err
	}
	caps := provider.Capabilities()
	runtime, ok := provider.(plugin.TableResultProvider)
	if !ok || caps.Compute == nil || caps.Compute.Query == nil || !caps.Compute.Query.Supported || !caps.Compute.Query.TableResult {
		return nil, fmt.Errorf("engine %s has no table result provider", engine.EngineType)
	}
	model, err := EngineCatalogModel(engine.EngineType)
	if err != nil {
		return nil, err
	}
	path, err := resourcetree.EngineCatalogPathFromLocator(model, target)
	if err != nil {
		return nil, err
	}
	return runtime.PrepareTableResult(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), plugin.TableResultRequest{
		Query:  plugin.QueryRequest{EngineID: engine.ID, Language: "sql", Query: query, Options: plugin.QueryOptions{ReadOnly: true, Parameters: parameters}},
		Target: path, WriteMode: mode,
	})
}
