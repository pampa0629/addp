package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/engineaccess"
	"github.com/addp/manager/internal/repository"
)

var ErrFileSourceAuthorizationRequired = fmt.Errorf("%w: exact file execution source authorization required", commonAPI.ErrForbidden)
var ErrModel3DGLBSourceCompositionIncomplete = fmt.Errorf("%w: model GLB source composition incomplete", ErrFileSourceAuthorizationRequired)

type model3DGLBSourceEngineClient interface {
	GetEngineForTenant(context.Context, uint, uint) (*commonModels.Engine, error)
}

func (s *Model3DGLBTaskService) SetSourceEngineClient(client model3DGLBSourceEngineClient) {
	s.sourceEngines = client
}

// sourceEngineForSnapshot classifies the actual System engine, never a caller
// supplied engine_type or the presence/absence of a preparation marker.
func (s *Model3DGLBTaskService) sourceEngineForSnapshot(ctx context.Context, snapshot *repository.FileTaskSnapshot) (*commonModels.Engine, error) {
	if s == nil || s.sourceEngines == nil || snapshot == nil || snapshot.TenantID == 0 || ctx.Err() != nil {
		return nil, engineaccess.ErrUnavailable
	}
	var header struct {
		Source struct {
			EngineID uint   `json:"source_engine_id"`
			Locator  string `json:"item_locator"`
		} `json:"source"`
	}
	if json.Unmarshal(snapshot.Config, &header) != nil || header.Source.EngineID == 0 {
		return nil, ErrFileSourceAuthorizationRequired
	}
	locator, err := resourcetree.ParseURI(header.Source.Locator)
	if err != nil || locator.EngineID != header.Source.EngineID {
		return nil, ErrFileSourceAuthorizationRequired
	}
	engine, err := s.sourceEngines.GetEngineForTenant(ctx, snapshot.TenantID, header.Source.EngineID)
	if err != nil || engineaccess.EnsureAvailable(engine) != nil {
		return nil, engineaccess.ErrUnavailable
	}
	if engine.ID != header.Source.EngineID || (engine.TenantID != nil && *engine.TenantID != snapshot.TenantID) {
		return nil, ErrFileSourceAuthorizationRequired
	}
	// Plugin lookup is case-insensitive. Reject malformed System types here,
	// rather than let "FTP" evade the exact FTP branch and resolve later.
	if engine.EngineType == "" || engine.EngineType != strings.ToLower(strings.TrimSpace(engine.EngineType)) {
		return nil, ErrFileSourceAuthorizationRequired
	}
	if commonExecution.ManagerModel3DSourceEngineType(engine.EngineType) && engine.TenantID == nil {
		return nil, ErrFileSourceAuthorizationRequired
	}
	return engine, nil
}

// validateModel3DGLBClaim checks only durable ownership and provenance. Passing
// this check never replaces current System scope consumption before source I/O.
func validateModel3DGLBClaim(ctx context.Context, item *commonExecution.TaskExecution) error {
	return validateFileSourceClaim(ctx, item, commonExecution.TaskTypeModel3DGLBGeneration)
}

func managedModel3DFileSource(engineType, format string) bool {
	return commonExecution.ManagerModel3DSourceEngineType(engineType) && commonExecution.ManagerModel3DConversionFormat(format)
}
