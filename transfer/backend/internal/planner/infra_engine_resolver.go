package planner

import (
	"fmt"
	"strings"

	"github.com/addp/common/infra/objectstore"
	objectcontent "github.com/addp/common/infra/objectstore/contentadapter"
)

type InfraEngineConfig struct{ ObjectArea objectstore.ContentArea }
type InfraEngineResolver struct{ cfg InfraEngineConfig }

func NewInfraEngineResolver(cfg InfraEngineConfig) *InfraEngineResolver {
	return &InfraEngineResolver{cfg: cfg}
}
func (r *InfraEngineResolver) ResolveEngine(ref EngineRef) (EngineBinding, error) {
	if r == nil || r.cfg.ObjectArea == nil {
		return EngineBinding{}, fmt.Errorf("platform object area is unavailable")
	}
	if ref.Type != "infra:object" {
		return EngineBinding{}, fmt.Errorf("unsupported infra engine ref type %q", ref.Type)
	}
	provider := &objectcontent.Provider{Name: objectstore.Manager, Store: r.cfg.ObjectArea}
	caps := provider.Capabilities()
	return EngineBinding{Type: provider.Type(), PluginType: provider.Type(), BoundProvider: provider, Capabilities: &caps}, nil
}

type HybridEngineResolver struct {
	system EngineResolver
	infra  EngineResolver
}

func NewHybridEngineResolver(system EngineResolver, infra EngineResolver) *HybridEngineResolver {
	return &HybridEngineResolver{system: system, infra: infra}
}

func (r *HybridEngineResolver) ResolveEngine(ref EngineRef) (EngineBinding, error) {
	if strings.HasPrefix(strings.TrimSpace(ref.Type), "infra:") {
		if r == nil || r.infra == nil {
			return EngineBinding{}, fmt.Errorf("infra engine resolver is required")
		}
		return r.infra.ResolveEngine(ref)
	}
	if r == nil || r.system == nil {
		return EngineBinding{}, fmt.Errorf("system engine resolver is required")
	}
	return r.system.ResolveEngine(ref)
}
