package service

import (
	"context"
	"errors"
	"time"

	commonmodels "github.com/addp/common/models"
)

var ErrInvalidGeoPythonMetrics = errors.New("invalid GeoPython metrics declaration")
var ErrGeoPythonMetricsConflict = errors.New("GeoPython metrics declaration does not match the current runtime")

func (s *EngineService) DeclareGeoPythonMetrics(ctx context.Context, engineID uint, claim commonmodels.GeoPythonMetricsDeclaration) error {
	now := time.Now().UTC()
	if engineID == 0 || claim.Validate(now) != nil {
		return ErrInvalidGeoPythonMetrics
	}
	accepted, err := s.repo.DeclareGeoPythonMetrics(ctx, commonmodels.GeoPythonMetricsIdentity{
		GeoPythonMetricsDeclaration: claim, EngineID: engineID,
		LeaseExpiresAt: now.Add(commonmodels.GeoPythonMetricsLeaseDuration),
	})
	if err != nil {
		return err
	}
	if !accepted {
		return ErrGeoPythonMetricsConflict
	}
	return nil
}
