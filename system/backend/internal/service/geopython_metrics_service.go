package service

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	commonmodels "github.com/addp/common/models"
)

var ErrInvalidRuntimeMetrics = errors.New("invalid runtime metrics declaration")
var ErrRuntimeMetricsConflict = errors.New("runtime metrics declaration does not match the current runtime")

func (s *EngineService) DeclareRuntimeMetrics(ctx context.Context, engineID uint, kind string, claim commonmodels.RuntimeMetricsDeclaration) error {
	now := time.Now().UTC()
	if commonmodels.RuntimeMetricsEngineType(kind) == "" || engineID == 0 || claim.Validate(now) != nil {
		return ErrInvalidRuntimeMetrics
	}
	if kind == "supermap" || kind == "jupyter" {
		expected := os.Getenv("ADDP_" + strings.ToUpper(kind) + "_METRICS_ENGINE_ID")
		if expected != strconv.FormatUint(uint64(engineID), 10) {
			return ErrRuntimeMetricsConflict
		}
	}
	accepted, err := s.repo.DeclareRuntimeMetrics(ctx, commonmodels.RuntimeMetricsIdentity{
		RuntimeMetricsDeclaration: claim, EngineID: engineID, Kind: kind,
		LeaseExpiresAt: now.Add(commonmodels.RuntimeMetricsLeaseDuration),
	})
	if err != nil {
		return err
	}
	if !accepted {
		return ErrRuntimeMetricsConflict
	}
	return nil
}
