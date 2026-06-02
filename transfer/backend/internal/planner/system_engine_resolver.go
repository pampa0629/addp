package planner

import (
	"fmt"
	"strings"

	commonmodels "github.com/addp/common/models"

	engineplugin "github.com/addp/common/engine/plugin"
)

type SystemEngineGetter interface {
	GetEngine(engineID uint) (*commonmodels.Engine, error)
}

type SystemEngineResolver struct {
	client SystemEngineGetter
}

func NewSystemEngineResolver(client SystemEngineGetter) *SystemEngineResolver {
	return &SystemEngineResolver{client: client}
}

func (r *SystemEngineResolver) ResolveEngine(ref EngineRef) (EngineBinding, error) {
	if r == nil || r.client == nil {
		return EngineBinding{}, fmt.Errorf("system engine resolver requires client")
	}
	if ref.ID == 0 {
		return EngineBinding{}, fmt.Errorf("engine id is required")
	}

	engine, err := r.client.GetEngine(ref.ID)
	if err != nil {
		return EngineBinding{}, err
	}
	if engine == nil {
		return EngineBinding{}, fmt.Errorf("engine %d not found", ref.ID)
	}
	if !engine.IsActive {
		return EngineBinding{}, fmt.Errorf("engine %d is inactive", ref.ID)
	}

	engineType := strings.TrimSpace(engine.EngineType)
	if engineType == "" {
		return EngineBinding{}, fmt.Errorf("engine %d has empty engine_type", ref.ID)
	}
	if ref.Type != "" && ref.Type != engineType {
		return EngineBinding{}, fmt.Errorf("engine %d type mismatch: task declares %q, system has %q", ref.ID, ref.Type, engineType)
	}

	return EngineBinding{
		Type:     engineType,
		ConnInfo: toEnginePluginConnInfo(engine.ConnectionInfo),
		EngineID: engine.ID,
	}, nil
}

func toEnginePluginConnInfo(connInfo commonmodels.ConnectionInfo) engineplugin.ConnectionInfo {
	result := make(engineplugin.ConnectionInfo, len(connInfo))
	for key, value := range connInfo {
		result[key] = value
	}
	return result
}
