package service

import (
	"context"
	"errors"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/engineaccess"
	"reflect"
	"strconv"
	"time"
)

var ErrFileSourceAuthorizationUnavailable = errors.New("exact file source authorization unavailable")

type fileSourceAuthorization struct {
	client          *commonClient.SystemServiceClient
	scope           commonExecution.ManagerFileReadScope
	lease           commonExecution.Lease
	authorizationID string
	expiresAt       time.Time
}

func (a *fileSourceAuthorization) Composition() *authorizationfacts.FileCompositionBoundary {
	if a == nil {
		return nil
	}
	return a.scope.FileComposition.Clone()
}

func (a *fileSourceAuthorization) BeforeSourceRead(ctx context.Context, target plugin.EngineCatalogPath) (*commonModels.Engine, error) {
	if a == nil {
		return nil, ErrFileSourceAuthorizationRequired
	}
	matched := false
	for _, ref := range a.scope.FileComposition.Refs {
		if reflect.DeepEqual(ref.Target, target) {
			matched = true
			break
		}
	}
	if !matched {
		return nil, ErrFileSourceAuthorizationRequired
	}
	return a.consume(ctx)
}

func (a *fileSourceAuthorization) Checkpoint(ctx context.Context) error {
	_, err := a.consume(ctx)
	return err
}
func (a *fileSourceAuthorization) BeforePublish(ctx context.Context) error {
	_, err := a.consume(ctx)
	return err
}

func (a *fileSourceAuthorization) consume(ctx context.Context) (*commonModels.Engine, error) {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if a == nil || !ok || lease != a.lease || !a.expiresAt.After(time.Now()) || a.scope.Validate() != nil {
		return nil, ErrFileSourceAuthorizationRequired
	}
	if ctx.Err() != nil || a.client == nil {
		return nil, ErrFileSourceAuthorizationUnavailable
	}
	request := commonClient.ManagerFileAccessRequest{ExecutionID: lease.ExecutionID, Attempt: lease.Attempt, LeaseToken: lease.Token, SourceReadScope: *a.scope.Clone()}
	var observation *commonClient.ManagerFileAccessObservation
	var err error
	switch a.scope.Purpose() {
	case commonExecution.TaskTypeModel3DGLBGeneration:
		observation, err = a.client.WithTenantID(uint(lease.TenantID)).CheckManagerModel3DAccess(ctx, a.authorizationID, request)
	case commonExecution.TaskTypeRasterCOGGeneration:
		observation, err = a.client.WithTenantID(uint(lease.TenantID)).CheckManagerRasterCOGAccess(ctx, a.authorizationID, request)
	default:
		return nil, ErrFileSourceAuthorizationRequired
	}
	if err != nil {
		var upstream *commonClient.SystemAPIError
		if errors.As(err, &upstream) && (upstream.StatusCode == 400 || upstream.StatusCode == 401 || upstream.StatusCode == 403 || upstream.StatusCode == 409) {
			return nil, ErrFileSourceAuthorizationRequired
		}
		return nil, ErrFileSourceAuthorizationUnavailable
	}
	if observation == nil || observation.ObservedAt.IsZero() || ctx.Err() != nil {
		return nil, ErrFileSourceAuthorizationUnavailable
	}
	engine := observation.Engine
	primary, err := a.scope.FileComposition.PrimaryTarget()
	if err != nil || engine == nil || !commonExecution.ManagerFileSourceComposition(engine.EngineType, &a.scope.FileComposition) || engine.ID != primary.EngineID || engine.TenantID == nil || int(*engine.TenantID) != lease.TenantID ||
		strconv.FormatInt(engine.Version, 10) != a.scope.EngineVersion || engineaccess.EnsureAvailable(engine) != nil {
		return nil, ErrFileSourceAuthorizationRequired
	}
	return engine, nil
}

func validateFileSourceClaim(ctx context.Context, item *commonExecution.TaskExecution, purpose string) error {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	now := time.Now().UTC()
	if ctx.Err() != nil || item == nil || !ok || item.TenantID <= 0 ||
		item.Module != commonExecution.ModuleManager || item.TaskType != purpose ||
		item.Source != commonExecution.ModuleManager || item.TriggerType != commonExecution.TriggerTypeManual || item.ParentExecutionID != nil || item.SourceTaskID == nil ||
		item.ExecutionBoundary != commonExecution.ExecutionBoundaryBounded || item.Status != commonExecution.ExecutionStatusRunning || item.Attempt <= 0 ||
		item.ExecutionAuthorizationID == nil || *item.ExecutionAuthorizationID <= 0 || item.AuthorizationExpiresAt == nil || !item.AuthorizationExpiresAt.After(now) ||
		item.LeaseExpiresAt == nil || !item.LeaseExpiresAt.After(now) || item.LeaseToken == nil || *item.LeaseToken == "" || item.LeaseOwner == nil || *item.LeaseOwner == "" ||
		lease.ExecutionID != item.ExecutionID || lease.TenantID != item.TenantID || lease.Attempt != item.Attempt || lease.Token != *item.LeaseToken || lease.Owner != *item.LeaseOwner {
		return ErrFileSourceAuthorizationRequired
	}
	if _, err := commonExecution.ParseSourceTaskIDUint(item.SourceTaskID); err != nil {
		return ErrFileSourceAuthorizationRequired
	}
	if err := commonExecution.CopyActorOrigin(&commonExecution.TaskExecution{TenantID: item.TenantID}, item); err != nil {
		return ErrFileSourceAuthorizationRequired
	}
	return nil
}
