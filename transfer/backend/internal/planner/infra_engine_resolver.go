package planner

import (
	"fmt"
	"strings"

	engineplugin "github.com/addp/common/engine/plugin"
)

type InfraEngineConfig struct {
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	MinioUseSSL    bool
}

type InfraEngineResolver struct {
	cfg InfraEngineConfig
}

func NewInfraEngineResolver(cfg InfraEngineConfig) *InfraEngineResolver {
	return &InfraEngineResolver{cfg: cfg}
}

func (r *InfraEngineResolver) ResolveEngine(ref EngineRef) (EngineBinding, error) {
	if r == nil {
		return EngineBinding{}, fmt.Errorf("infra engine resolver is required")
	}
	switch strings.TrimSpace(ref.Type) {
	case infraEngineRefType("minio"):
		if strings.TrimSpace(r.cfg.MinioEndpoint) == "" {
			return EngineBinding{}, fmt.Errorf("infra minio endpoint is required")
		}
		return EngineBinding{
			Type:       "minio",
			PluginType: "minio",
			ConnInfo: engineplugin.ConnectionInfo{
				"endpoint":   r.cfg.MinioEndpoint,
				"access_key": r.cfg.MinioAccessKey,
				"secret_key": r.cfg.MinioSecretKey,
				"use_ssl":    r.cfg.MinioUseSSL,
			},
		}, nil
	default:
		return EngineBinding{}, fmt.Errorf("unsupported infra engine ref type %q", ref.Type)
	}
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
