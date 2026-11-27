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
	binding, err := availableEngineBinding(ref, engine)
	if err == nil {
		binding.ConnInfo = toEnginePluginConnInfo(engine.ConnectionInfo)
	}
	return binding, err
}

// DescriptorEngineResolver supplies only control-plane facts for task validation.
type DescriptorEngineResolver struct {
	Context  context.Context
	TenantID uint
	Lookup   func(context.Context, uint, uint) (*commonmodels.EngineRuntimeDescriptor, error)
}

func (r DescriptorEngineResolver) ResolveEngine(ref EngineRef) (EngineBinding, error) {
	if r.Lookup == nil || r.TenantID == 0 || ref.ID == 0 {
		return EngineBinding{}, fmt.Errorf("task validation requires a tenant and an engine descriptor")
	}
	d, err := r.Lookup(r.Context, r.TenantID, ref.ID)
	if err != nil {
		return EngineBinding{}, err
	}
	if d == nil || d.ID != ref.ID {
		return EngineBinding{}, fmt.Errorf("missing descriptor for engine %d", ref.ID)
	}
	return availableEngineBinding(ref, &commonmodels.Engine{
		ID: d.ID, Version: d.Version, EngineType: d.EngineType,
		LifecycleState: d.LifecycleState, ConnectionStatus: d.ConnectionStatus, Capabilities: d.Capabilities,
	})
}

func availableEngineBinding(ref EngineRef, engine *commonmodels.Engine) (EngineBinding, error) {
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
		Type:          engineType,
		EngineID:      engine.ID,
		EngineVersion: engine.Version,
		Capabilities:  capabilities,
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

func EngineBindingFromEngine(engine *commonmodels.Engine) (EngineBinding, error) {
	if engine == nil || engine.ID == 0 || strings.TrimSpace(engine.EngineType) == "" {
		return EngineBinding{}, fmt.Errorf("authorized engine is invalid")
	}
	capabilities, err := engineCapabilities(engine)
	if err != nil {
		return EngineBinding{}, fmt.Errorf("parse engine %d capabilities: %w", engine.ID, err)
	}
	return EngineBinding{
		Type: strings.TrimSpace(engine.EngineType), ConnInfo: toEnginePluginConnInfo(engine.ConnectionInfo),
		EngineID: engine.ID, EngineVersion: engine.Version, Capabilities: capabilities,
	}, nil
}
