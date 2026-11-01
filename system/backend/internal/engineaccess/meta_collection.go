package engineaccess

import (
	"context"
	"errors"
	"github.com/addp/common/execution"
	"github.com/addp/system/internal/models"
)

var ErrMetaCollectionFactsUnavailable = errors.New("Meta collection facts unavailable")

type MetaScanFactsReader interface {
	ReadScanPlanFacts(context.Context, uint, string) (*execution.MetaScanPlanFacts, error)
}

func (s *Service) WithMetaScanFactsReader(reader MetaScanFactsReader) *Service {
	s.metaScanFacts = reader
	return s
}
func collectionEngineAvailable(engine *models.Engine) bool {
	return engine != nil && engine.EngineType == "postgresql" && engine.Version > 0 && engine.LifecycleState == models.EngineLifecycleActive
}
