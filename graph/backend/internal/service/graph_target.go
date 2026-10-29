package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/propertygraph"
	engineselection "github.com/addp/common/engine/selection"
	commonmodels "github.com/addp/common/models"
)

type graphTarget struct {
	*commonmodels.Engine
	provider      plugin.PropertyGraphProvider
	conn          plugin.ConnectionInfo
	path          plugin.EngineCatalogPath
	managed       bool
	endProtection func()
}

func (s *GraphDataService) resolveTarget(ctx context.Context, tenantID, engineID uint, path plugin.EngineCatalogPath) (*graphTarget, error) {
	if engineID == 0 || path.EngineID != engineID {
		return nil, fmt.Errorf("graph target engine identity is required")
	}
	engine, err := s.systemClient.WithTenantID(tenantID).GetEngine(ctx, engineID)
	if err != nil {
		return nil, err
	}
	if input, ok := buildInputFromContext(ctx); ok && (input.EngineID != engineID || input.EngineVersion != engine.Version || !sameCatalogTarget(input.CatalogPath, path)) {
		return nil, ErrGraphTargetInvalid
	}
	if !engineselection.IsAvailableForComputeEntrypoint(engine, "query") {
		return nil, fmt.Errorf("graph engine is unavailable")
	}
	target, err := bindGraphTarget(engine, path)
	if err != nil {
		return nil, err
	}
	end, err := s.protection.BeginTarget(ctx, tenantID, target.provider, target.path)
	if err != nil {
		return nil, err
	}
	target.endProtection = end
	return target, nil
}
func bindGraphTarget(engine *commonmodels.Engine, path plugin.EngineCatalogPath) (*graphTarget, error) {
	registered, err := plugin.Get(engine.EngineType)
	if err != nil {
		return nil, err
	}
	provider, ok := registered.(plugin.PropertyGraphProvider)
	if !ok {
		return nil, fmt.Errorf("engine does not support property graph operations")
	}
	model, ok := registered.(plugin.EngineCatalogModelProvider)
	if !ok {
		return nil, fmt.Errorf("graph catalog model is required")
	}
	if path.EngineID != engine.ID || propertygraph.ValidateTarget(path, model.EngineCatalogModel()) != nil {
		return nil, fmt.Errorf("invalid graph catalog path")
	}
	segments := plugin.EngineCatalogPathWithoutRoot(path).Segments
	if len(segments) == 0 || segments[len(segments)-1].Kind != plugin.EngineCatalogKindGraph {
		return nil, fmt.Errorf("graph target must be a graph leaf")
	}
	conn := plugin.ConnectionInfo{}
	for k, v := range engine.ConnectionInfo {
		conn[k] = v
	}
	return &graphTarget{Engine: engine, provider: provider, conn: conn, path: path}, nil
}
func (t *graphTarget) query(ctx context.Context, query string, params map[string]interface{}) (*plugin.GraphQueryResult, error) {
	request, err := json.Marshal(struct {
		Query      string
		Parameters map[string]interface{}
	}{query, params})
	if err != nil {
		return nil, err
	}
	return recordExternalCall(ctx, "graph", request, func() (*plugin.GraphQueryResult, error) {
		result, err := t.provider.ExecuteGraphQueryAtPath(ctx, t.conn, t.path, query, plugin.QueryOptions{Parameters: params})
		if err == nil && result == nil {
			err = fmt.Errorf("graph provider returned no completion result")
		}
		return result, err
	})
}
func executeGraphQuery(ctx context.Context, target *graphTarget, query string) (*plugin.GraphQueryResult, error) {
	return target.query(ctx, query, nil)
}

func (t *graphTarget) close() {
	if t != nil && t.endProtection != nil {
		t.endProtection()
	}
}
