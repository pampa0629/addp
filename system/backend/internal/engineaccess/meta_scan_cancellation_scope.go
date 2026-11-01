package engineaccess

import (
	"context"
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
)

// GetMetaScanCancellationScope checks current cancel permission and engine
// management scope. It neither approves a task nor grants source access.
func (s *Service) GetMetaScanCancellationScope(ctx context.Context, actor Actor, engineID int64) (*authorization.EngineAccessHandlingScope, error) {
	var result *authorization.EngineAccessHandlingScope
	err := s.withEngineManagementScope(ctx, actor, engineID, "meta.scan_task.cancel", false, func(tx *Repository, check func() error) error {
		engine, err := tx.engine(ctx, actor.TenantID, engineID, false)
		if err != nil {
			return err
		}
		// Cancellation remains available when an administrator disables the engine.
		// The shared management check still rejects deleted or purging engines.
		if engine == nil || engine.EngineType != "postgresql" || engine.Version <= 0 {
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
