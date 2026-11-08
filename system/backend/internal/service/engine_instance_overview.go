package service

import (
	"context"
	"errors"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/models"
)

var (
	ErrInstanceOverviewUnsupported = errors.New("engine instance overview is unsupported")
	ErrInstanceOverviewUnavailable = errors.New("engine instance overview is unavailable")
)

func (s *EngineService) ReadInstanceOverview(ctx context.Context, id, tenantID uint) (*models.EngineInstanceOverviewResponse, error) {
	engine, err := s.GetForConnection(id, tenantID)
	if err != nil {
		return nil, err
	}
	native, err := plugin.Get(engine.EngineType)
	if err != nil {
		return nil, ErrInstanceOverviewUnsupported
	}
	provider, ok := native.(plugin.InstanceOverviewProvider)
	if !ok || !native.Capabilities().InstanceOverview {
		return nil, ErrInstanceOverviewUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := provider.ReadInstanceOverview(ctx, plugin.ConnectionInfo(engine.ConnectionInfo))
	if err != nil {
		return nil, ErrInstanceOverviewUnavailable
	}
	return &models.EngineInstanceOverviewResponse{EngineID: engine.ID, Version: engine.Version, QueriedAt: time.Now().UTC(), Metrics: result.Metrics}, nil
}
