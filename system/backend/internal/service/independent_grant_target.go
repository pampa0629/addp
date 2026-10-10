package service

import (
	"bytes"
	"context"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/dbbridge"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/models"
)

// Adapter to the existing System-owned live Engine Catalog Provider, not the
// enterprise Catalog module. It never fetches samples or writes to the source.
type IndependentGrantTargetVerifier struct {
	engines interface {
		GetForConnection(uint, uint) (*models.Engine, error)
	}
	resolve func(context.Context, *models.Engine, engineplugin.EngineCatalogPath) (*engineplugin.EngineCatalogEntry, error)
}

func NewIndependentGrantTargetVerifier(engines *EngineService) *IndependentGrantTargetVerifier {
	return &IndependentGrantTargetVerifier{engines: engines, resolve: dbbridge.ResolveEngineCatalogPath}
}

func (v *IndependentGrantTargetVerifier) VerifyIndependentReadTarget(ctx context.Context, tenantID int64, path engineplugin.EngineCatalogPath) (int64, error) {
	engine, err := v.engines.GetForConnection(path.EngineID, uint(tenantID))
	if err != nil {
		return 0, engineaccess.ErrIndependentGrantSourceUnavailable
	}
	if engine.ID != path.EngineID || engine.TenantID == nil || int64(*engine.TenantID) != tenantID || engine.LifecycleState != models.EngineLifecycleActive {
		return 0, commonapi.ErrForbidden
	}
	wanted, err := shared.EncodeSharingTarget(path)
	if err != nil {
		return 0, commonapi.ErrBadRequest
	}
	entry, err := v.resolve(ctx, engine, path)
	if err != nil {
		return 0, engineaccess.ErrIndependentGrantSourceUnavailable
	}
	if entry == nil || entry.Role != engineplugin.EngineCatalogRoleLeaf ||
		(entry.Kind != engineplugin.EngineCatalogKindTable && entry.Kind != engineplugin.EngineCatalogKindCollection) || entry.Term != entry.Kind {
		return 0, commonapi.ErrBadRequest
	}
	actual, actualErr := shared.EncodeSharingTarget(entry.Path)
	last := path.Segments[len(path.Segments)-1]
	if actualErr != nil || !bytes.Equal(wanted, actual) || last.Term != entry.Term || last.Kind != entry.Kind {
		return 0, commonapi.ErrBadRequest
	}
	return engine.Version, nil
}
