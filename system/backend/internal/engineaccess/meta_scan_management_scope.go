package engineaccess

import (
	"context"
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
)

// GetMetaScanManagementScope checks the current operation permission and engine
// management scope. It neither approves a task nor grants source access.
func (s *Service) GetMetaScanManagementScope(ctx context.Context, actor Actor, engineID int64, operation string) (*authorization.EngineAccessHandlingScope, error) {
	var permission string
	switch operation {
	case "create":
		permission = "meta.scan_task.create"
	case "update":
		permission = "meta.scan_task.update"
	case "delete":
		permission = "meta.scan_task.delete"
	case "execute":
		permission = "meta.scan_task.execute"
	case "cancel":
		permission = "meta.scan_task.cancel"
	default:
		return nil, commonapi.ErrBadRequest
	}
	var result *authorization.EngineAccessHandlingScope
	err := s.withEngineManagementScope(ctx, actor, engineID, permission, false, func(tx *Repository, check func() error) error {
		engine, err := tx.engine(ctx, actor.TenantID, engineID, false)
		if err != nil {
			return err
		}
		// Cancellation remains available when an administrator disables the engine.
		// The shared management check still rejects deleted or purging engines.
		if engine == nil || engine.EngineType != "postgresql" || engine.Version <= 0 {
			return commonapi.ErrForbidden
		}
		if operation != "cancel" && operation != "delete" && !collectionEngineAvailable(engine) {
			return commonapi.ErrForbidden
		}
		now, err := tx.wallClock(ctx)
		if err != nil {
			return err
		}
		if err := check(); err != nil {
			return err
		}
		result = &authorization.EngineAccessHandlingScope{TenantID: actor.TenantID, EngineID: engineID, Operator: authorization.SharingFulfillmentOperator{PrincipalID: actor.PrincipalID, MembershipID: actor.MembershipID, AuthorizationVersion: actor.AuthorizationVersion}, VerifiedAt: now}
		return nil
	})
	return result, err
}
