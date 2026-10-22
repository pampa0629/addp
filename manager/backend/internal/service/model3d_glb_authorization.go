package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/engineaccess"
	"github.com/addp/manager/internal/repository"
)

var ErrModel3DGLBSourceAuthorizationRequired = fmt.Errorf("%w: model GLB execution source authorization required", commonAPI.ErrForbidden)

type model3DGLBSourceEngineClient interface {
	GetEngineForTenant(context.Context, uint, uint) (*commonModels.Engine, error)
}

func (s *Model3DGLBTaskService) SetSourceEngineClient(client model3DGLBSourceEngineClient) {
	s.sourceEngines = client
}

// sourceEngineForSnapshot classifies the actual System engine, never a caller
// supplied engine_type or the presence/absence of a preparation marker.
func (s *Model3DGLBTaskService) sourceEngineForSnapshot(ctx context.Context, snapshot *repository.Model3DGLBTaskSnapshot) (*commonModels.Engine, error) {
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
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	locator, err := resourcetree.ParseURI(header.Source.Locator)
	if err != nil || locator.EngineID != header.Source.EngineID {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	engine, err := s.sourceEngines.GetEngineForTenant(ctx, snapshot.TenantID, header.Source.EngineID)
	if err != nil || engineaccess.EnsureAvailable(engine) != nil {
		return nil, engineaccess.ErrUnavailable
	}
	if engine.ID != header.Source.EngineID || (engine.TenantID != nil && *engine.TenantID != snapshot.TenantID) {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	// Plugin lookup is case-insensitive. Reject malformed System types here,
	// rather than let "FTP" evade the exact FTP branch and resolve later.
	if engine.EngineType == "" || engine.EngineType != strings.ToLower(strings.TrimSpace(engine.EngineType)) {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	if commonExecution.ManagerModel3DSourceEngineType(engine.EngineType) && engine.TenantID == nil {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	return engine, nil
}

// validateModel3DGLBClaim checks only durable ownership and provenance. Passing
// this check never replaces current System scope consumption before source I/O.
func validateModel3DGLBClaim(ctx context.Context, item *commonExecution.TaskExecution) error {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	now := time.Now().UTC()
	if ctx.Err() != nil || item == nil || !ok || item.TenantID <= 0 ||
		item.Module != commonExecution.ModuleManager || item.TaskType != commonExecution.TaskTypeModel3DGLBGeneration ||
		item.Source != commonExecution.ModuleManager || item.TriggerType != commonExecution.TriggerTypeManual || item.ParentExecutionID != nil || item.SourceTaskID == nil ||
		item.ExecutionBoundary != commonExecution.ExecutionBoundaryBounded || item.Status != commonExecution.ExecutionStatusRunning || item.Attempt <= 0 ||
		item.ExecutionAuthorizationID == nil || *item.ExecutionAuthorizationID <= 0 || item.AuthorizationExpiresAt == nil || !item.AuthorizationExpiresAt.After(now) ||
		item.LeaseExpiresAt == nil || !item.LeaseExpiresAt.After(now) || item.LeaseToken == nil || *item.LeaseToken == "" || item.LeaseOwner == nil || *item.LeaseOwner == "" ||
		lease.ExecutionID != item.ExecutionID || lease.TenantID != item.TenantID || lease.Attempt != item.Attempt || lease.Token != *item.LeaseToken || lease.Owner != *item.LeaseOwner {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	if _, err := commonExecution.ParseSourceTaskIDUint(item.SourceTaskID); err != nil {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	if err := commonExecution.CopyActorOrigin(&commonExecution.TaskExecution{TenantID: item.TenantID}, item); err != nil {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	return nil
}

func managedModel3DFileSource(engineType, format string) bool {
	return commonExecution.ManagerModel3DSourceFormat(engineType, format)
}
