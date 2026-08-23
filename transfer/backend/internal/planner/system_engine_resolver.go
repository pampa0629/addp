package planner

import (
	"context"
	"fmt"
	"strings"

	commonmodels "github.com/addp/common/models"

	engineplugin "github.com/addp/common/engine/plugin"
	engineselection "github.com/addp/common/engine/selection"
)

type SystemEngineGetter interface {
	GetEngineForTenant(ctx context.Context, tenantID, engineID uint) (*commonmodels.Engine, error)
}

type SystemEngineResolver struct {
	client   SystemEngineGetter
	tenantID uint
}

func NewSystemEngineResolver(client SystemEngineGetter) *SystemEngineResolver {
	return &SystemEngineResolver{client: client}
}

func (r *SystemEngineResolver) ForTenant(tenantID uint) EngineResolver {
	if r == nil {
		return (*SystemEngineResolver)(nil)
	}
	return &SystemEngineResolver{client: r.client, tenantID: tenantID}
}

type TenantBindableEngineResolver interface {
	ForTenant(tenantID uint) EngineResolver
}

func BindEngineResolver(resolver EngineResolver, tenantID uint) EngineResolver {
	if bindable, ok := resolver.(TenantBindableEngineResolver); ok {
		return bindable.ForTenant(tenantID)
	}
	return resolver
}

func (r *SystemEngineResolver) ResolveEngine(ref EngineRef) (EngineBinding, error) {
	if r == nil || r.client == nil || r.tenantID == 0 {
		return EngineBinding{}, fmt.Errorf("system engine resolver requires client")
	}
	if ref.ID == 0 {
		return EngineBinding{}, fmt.Errorf("engine id is required")
	}

	engine, err := r.client.GetEngineForTenant(context.Background(), r.tenantID, ref.ID)
	if err != nil {
		return EngineBinding{}, err
	}
	if engine == nil {
		return EngineBinding{}, fmt.Errorf("engine %d not found", ref.ID)
	}
	if !engineselection.IsAvailable(engine) {
		return EngineBinding{}, fmt.Errorf("engine %d is unavailable", ref.ID)
	}

	engineType := strings.TrimSpace(engine.EngineType)
	if engineType == "" {
		return EngineBinding{}, fmt.Errorf("engine %d has empty engine_type", ref.ID)
	}
	if ref.Type != "" && ref.Type != engineType {
		return EngineBinding{}, fmt.Errorf("engine %d type mismatch: task declares %q, system has %q", ref.ID, ref.Type, engineType)
	}

	capabilities, err := engineCapabilities(engine)
	if err != nil {
		return EngineBinding{}, fmt.Errorf("parse engine %d capabilities: %w", engine.ID, err)
	}
	return EngineBinding{
		Type:         engineType,
		ConnInfo:     toEnginePluginConnInfo(engine.ConnectionInfo),
		EngineID:     engine.ID,
		Capabilities: capabilities,
	}, nil
}

func engineCapabilities(engine *commonmodels.Engine) (*engineplugin.EngineCapabilities, error) {
	if engine == nil || engine.Capabilities == nil || *engine.Capabilities == "" {
		return nil, nil
	}
	capabilities, err := engineplugin.ParseEngineCapabilities(string(*engine.Capabilities))
	if err != nil {
		return nil, err
	}
	return capabilities, nil
}

func toEnginePluginConnInfo(connInfo commonmodels.ConnectionInfo) engineplugin.ConnectionInfo {
	result := make(engineplugin.ConnectionInfo, len(connInfo))
	for key, value := range connInfo {
		result[key] = value
	}
	return result
}
