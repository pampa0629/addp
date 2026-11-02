package cleanup

import (
	"context"
	"strconv"

	"github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	"gorm.io/gorm"
)

// EngineConnection obtains a separate exact-object decision outside C, then
// rechecks the local live attempt. It neither authorizes nor dispatches effects.
func (r *Run) EngineConnection(ctx context.Context, engineID uint) (*client.CleanupEngineConnection, error) {
	if r == nil || r.runner == nil || r.runner.DB == nil || r.runner.System == nil || r.runner.Owner != events.ModuleTransfer || r.Execution.ExecutionAuthorizationID == nil || r.Execution.Module != events.ModuleTransfer || r.Execution.TenantID <= 0 || r.Input.TenantID != uint(r.Execution.TenantID) || r.Lease.ExecutionID != r.Execution.ExecutionID || r.Input.Validate() != nil || r.Input.Action != events.CleanupActionExecute || r.Input.CauseEvent != events.CleanupCauseEngineDeleting || r.Input.Context.EngineID != engineID || engineID == 0 {
		return nil, &authorizationFailure{code: "cleanup_authorization_denied"}
	}
	scope, err := execution.CleanupAuthorizationFor(r.Execution)
	if err != nil || r.Input.Digest != scope.RequestDigest || r.Input.RequestID != scope.RequestID || r.Input.ParentExecutionID != scope.ParentExecutionID {
		return nil, &authorizationFailure{code: "cleanup_authorization_denied"}
	}
	grant, err := r.runner.System.WithTenantID(uint(r.Execution.TenantID)).GetCleanupEngineConnection(ctx, strconv.FormatInt(*r.Execution.ExecutionAuthorizationID, 10), client.CleanupEngineConnectionRequest{
		ExecutionID: r.Execution.ExecutionID, Attempt: r.Lease.Attempt, LeaseToken: r.Lease.Token, Scope: scope, EngineID: strconv.FormatUint(uint64(engineID), 10),
	})
	if err != nil {
		return nil, &authorizationFailure{code: authorizationFailureCode(err)}
	}
	err = r.runner.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := execution.LockActiveLease(ctx, tx, r.Lease)
		if err != nil {
			return err
		}
		current, err := execution.CleanupAuthorizationFor(*item)
		if err != nil || current != grant.Scope || item.ExecutionAuthorizationID == nil || *item.ExecutionAuthorizationID != *r.Execution.ExecutionAuthorizationID || item.AuthorizationExpiresAt == nil || item.LeaseExpiresAt == nil || grant.ExpiresAt.After(*item.AuthorizationExpiresAt) || grant.ExpiresAt.After(*item.LeaseExpiresAt) {
			return &authorizationFailure{code: "cleanup_authorization_denied"}
		}
		now, err := execution.DatabaseNow(tx)
		if err != nil {
			return err
		}
		if !grant.ExpiresAt.After(now) {
			return &authorizationFailure{code: "cleanup_authorization_unavailable"}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return grant, nil
}
