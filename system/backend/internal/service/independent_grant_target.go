package service

import (
	"bytes"
	"context"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/models"
)

// Adapter to the existing System-owned live Engine Catalog Provider, not the
// enterprise Catalog module. It never fetches samples or writes to the source.
type IndependentGrantTargetVerifier struct {
	engines *EngineService
	storage *StorageEngineService
}

func NewIndependentGrantTargetVerifier(engines *EngineService) *IndependentGrantTargetVerifier {
	return &IndependentGrantTargetVerifier{engines: engines, storage: NewStorageEngineService()}
}

func (v *IndependentGrantTargetVerifier) VerifyIndependentTable(ctx context.Context, tenantID int64, path engineplugin.EngineCatalogPath) (int64, error) {
	engine, err := v.engines.GetForConnection(path.EngineID, uint(tenantID))
	if err != nil {
		return 0, engineaccess.ErrIndependentGrantSourceUnavailable
	}
	if engine.TenantID == nil || int64(*engine.TenantID) != tenantID || engine.LifecycleState != models.EngineLifecycleActive {
		return 0, commonapi.ErrForbidden
	}
	segments := make([]models.EngineCatalogSegment, 0, len(path.Segments))
	for _, segment := range path.Segments {
		segments = append(segments, models.EngineCatalogSegment{Term: segment.Term, Kind: segment.Kind, Name: segment.Name})
	}
	facts, err := v.storage.DescribeEngineCatalogFacts(ctx, engine, models.EngineCatalogDescribeFactsRequest{
		Path: models.EngineCatalogPath{Version: path.Version, EngineID: path.EngineID, Segments: segments}})
	if err != nil {
		return 0, engineaccess.ErrIndependentGrantSourceUnavailable
	}
	if facts == nil || facts.Kind != "table" || facts.Table == nil {
		return 0, commonapi.ErrBadRequest
	}
	wanted, err := shared.EncodeSharingTarget(path)
	actual, actualErr := shared.EncodeSharingTarget(facts.Path)
	if err != nil || actualErr != nil || !bytes.Equal(wanted, actual) {
		return 0, commonapi.ErrBadRequest
	}
	return engine.Version, nil
}
