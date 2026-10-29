package execution

import (
	"context"
	"fmt"
	"time"

	commonapi "github.com/addp/common/api"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CreatePending stores a new immutable execution identity. Owner input freezing,
// actor authorization and task admission belong to the same caller transaction.
func CreatePending(ctx context.Context, db *gorm.DB, item *TaskExecution) error {
	if db == nil || item == nil || item.ExecutionID == "" || item.TenantID <= 0 || item.Module == "" || item.TaskType == "" || item.Source == "" || item.Status != ExecutionStatusPending || item.ExecutionBoundary != ExecutionBoundaryBounded || item.Attempt != 0 || item.StartedAt != nil || item.CompletedAt != nil || item.ExecutionTimeMs != nil || item.LeaseOwner != nil || item.LeaseToken != nil || item.LeaseExpiresAt != nil {
		return fmt.Errorf("new bounded execution requires a complete pending identity without attempt facts")
	}
	if _, exists := item.Metadata["cleanup_receipt"]; exists {
		return fmt.Errorf("pending execution cannot contain a completion receipt")
	}
	return db.WithContext(ctx).Create(item).Error
}

// RejectUnstarted closes admission without manufacturing an attempt. The owner
// must first prove trusted provenance, permanent closure and no operation facts
// while holding the protocol's required parent/domain locks.
func RejectUnstarted(ctx context.Context, db *gorm.DB, identity PendingIdentity, fields map[string]interface{}) error {
	return finishUnstarted(ctx, db, identity, ExecutionStatusFailed, fields)
}

// CancelUnstarted is only for an owner contract that proves the first pending
// execution has never created a runner or operation, with claim excluded.
func CancelUnstarted(ctx context.Context, db *gorm.DB, identity PendingIdentity) error {
	return finishUnstarted(ctx, db, identity, ExecutionStatusCancelled, nil)
}

func exactPending(db *gorm.DB, identity PendingIdentity) *gorm.DB {
	return db.Session(&gorm.Session{}).Model(&TaskExecution{}).
		Where("execution_id = ? AND tenant_id = ? AND module = ? AND task_type = ? AND source = ?", identity.ExecutionID, identity.TenantID, identity.Module, identity.TaskType, identity.Source).
		Where("execution_boundary = ? AND status = ? AND started_at IS NULL AND completed_at IS NULL AND execution_time_ms IS NULL AND lease_owner IS NULL AND lease_token IS NULL AND lease_expires_at IS NULL", ExecutionBoundaryBounded, ExecutionStatusPending)
}

// FailPending rejects a queued execution, including a safely recovered attempt.
// It makes no claim that earlier attempts or external effects never existed.
func FailPending(ctx context.Context, db *gorm.DB, identity PendingIdentity, fields map[string]interface{}) error {
	return finishPending(ctx, db, identity, ExecutionStatusFailed, fields, false)
}

func finishUnstarted(ctx context.Context, db *gorm.DB, identity PendingIdentity, status string, input map[string]interface{}) error {
	return finishPending(ctx, db, identity, status, input, true)
}

func finishPending(ctx context.Context, db *gorm.DB, identity PendingIdentity, status string, input map[string]interface{}, neverStarted bool) error {
	if db == nil || identity.ExecutionID == "" || identity.TenantID <= 0 || identity.Module == "" || identity.TaskType == "" || identity.Source == "" {
		return fmt.Errorf("unstarted termination requires an exact identity")
	}
	fields, err := copyExecutionFields(input)
	if err != nil {
		return err
	}
	for key := range fields {
		if key != "error_details" {
			return fmt.Errorf("unstarted rejection cannot write %s", key)
		}
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := exactPending(tx, identity)
		if neverStarted {
			query = query.Where("attempt = 0 AND progress = 0")
		}
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var item TaskExecution
		if err := query.First(&item).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("%w: execution is not an unstarted pending candidate", commonapi.ErrConflict)
			}
			return err
		}
		if _, exists := item.Metadata["cleanup_receipt"]; exists {
			return fmt.Errorf("%w: execution already has operation evidence", commonapi.ErrConflict)
		}
		now, err := DatabaseNow(tx)
		if err != nil {
			return err
		}
		fields["status"], fields["completed_at"], fields["updated_at"] = status, now, now
		update := exactPending(tx, identity)
		if neverStarted {
			update = update.Where("attempt = 0 AND progress = 0")
		}
		return requireOwnedRow(update.Updates(fields), identity.ExecutionID, "terminate pending execution")
	})
}

// AttachAuthorizationWithLease attaches a System-issued reference once. Actor
// lineage is immutable; issuance and permission checking remain in System.
func AttachAuthorizationWithLease(ctx context.Context, db *gorm.DB, lease Lease, authorizationID int64, expiresAt time.Time) error {
	if authorizationID <= 0 || expiresAt.IsZero() {
		return fmt.Errorf("execution authorization reference is invalid")
	}
	return withLockedAttempt(ctx, db, lease, false, func(tx *gorm.DB, item TaskExecution, now time.Time) error {
		if item.ActorPrincipalID == nil || item.ActorTenantMembershipID == nil || item.IssuedAuthorizationVersion == nil || item.ExecutionAuthorizationID != nil || item.AuthorizationExpiresAt != nil || !expiresAt.After(now) {
			return fmt.Errorf("%w: execution authorization attachment is not admissible", commonapi.ErrConflict)
		}
		query := ownedActiveExecution(tx, lease, now).Where("execution_authorization_id IS NULL AND authorization_expires_at IS NULL")
		if tx.Dialector.Name() == "postgres" {
			query = query.Where("clock_timestamp() < ?", expiresAt)
		}
		return requireOwnedRow(query.Updates(map[string]interface{}{"execution_authorization_id": authorizationID, "authorization_expires_at": expiresAt.UTC(), "updated_at": now}), lease.ExecutionID, "attach authorization")
	})
}

// AttachPendingAuthorization binds one System-issued reference to a never-claimed
// bounded execution. The owner must verify issuance scope and immutable actor facts.
type PendingAuthorization struct {
	ID                         int64
	ExpiresAt                  time.Time
	ActorPrincipalID           int64
	ActorTenantMembershipID    int64
	IssuedAuthorizationVersion int64
}

func AttachPendingAuthorization(ctx context.Context, db *gorm.DB, identity PendingIdentity, authorization PendingAuthorization) error {
	authorizationID, expiresAt := authorization.ID, authorization.ExpiresAt
	if db == nil || identity.ExecutionID == "" || identity.TenantID <= 0 || identity.Module == "" || identity.TaskType == "" || identity.Source == "" || authorizationID <= 0 || expiresAt.IsZero() || authorization.ActorPrincipalID <= 0 || authorization.ActorTenantMembershipID <= 0 || authorization.IssuedAuthorizationVersion <= 0 {
		return fmt.Errorf("pending authorization requires complete identity and reference")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := exactPending(tx, identity).Where("attempt = 0 AND execution_authorization_id IS NULL AND authorization_expires_at IS NULL")
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var item TaskExecution
		if err := query.First(&item).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("%w: pending authorization is no longer admissible", commonapi.ErrConflict)
			}
			return err
		}
		now, err := DatabaseNow(tx)
		if err != nil {
			return err
		}
		noOrigin := item.ActorPrincipalID == nil && item.ActorTenantMembershipID == nil && item.IssuedAuthorizationVersion == nil
		matchesOrigin := item.ActorPrincipalID != nil && *item.ActorPrincipalID == authorization.ActorPrincipalID && item.ActorTenantMembershipID != nil && *item.ActorTenantMembershipID == authorization.ActorTenantMembershipID && item.IssuedAuthorizationVersion != nil && *item.IssuedAuthorizationVersion == authorization.IssuedAuthorizationVersion
		if (!noOrigin && !matchesOrigin) || (item.TriggeredBy != nil && int64(*item.TriggeredBy) != authorization.ActorPrincipalID) || !expiresAt.After(now) {
			return fmt.Errorf("%w: pending authorization lineage or expiry is invalid", commonapi.ErrConflict)
		}
		update := exactPending(tx, identity).Where("attempt = 0 AND execution_authorization_id IS NULL AND authorization_expires_at IS NULL")
		if tx.Dialector.Name() == "postgres" {
			update = update.Where("clock_timestamp() < ?", expiresAt)
		}
		return requireOwnedRow(update.Updates(map[string]interface{}{"execution_authorization_id": authorizationID, "authorization_expires_at": expiresAt.UTC(), "updated_at": now, "actor_principal_id": authorization.ActorPrincipalID, "actor_tenant_membership_id": authorization.ActorTenantMembershipID, "issued_authorization_version": authorization.IssuedAuthorizationVersion, "triggered_by": authorization.ActorPrincipalID}), identity.ExecutionID, "attach pending authorization")
	})
}

// FailUnleased closes an invalid bounded running record without inventing
// ownership. A complete attempt must instead use the active or expired lease API.
func FailUnleased(ctx context.Context, db *gorm.DB, executionID string, tenantID int, module, taskType string, input map[string]interface{}) error {
	if db == nil || executionID == "" || tenantID <= 0 || module == "" || taskType == "" {
		return fmt.Errorf("unleased recovery requires complete execution identity")
	}
	fields, err := copyExecutionFields(input)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		exact := func() *gorm.DB {
			return tx.Session(&gorm.Session{}).Model(&TaskExecution{}).Where("execution_id = ? AND tenant_id = ? AND module = ? AND task_type = ? AND execution_boundary = ? AND status = ? AND (attempt <= 0 OR lease_token IS NULL OR lease_owner IS NULL OR lease_owner = '' OR lease_expires_at IS NULL)", executionID, tenantID, module, taskType, ExecutionBoundaryBounded, ExecutionStatusRunning)
		}
		query := exact()
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var item TaskExecution
		if err := query.First(&item).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("%w: execution is not an invalid unleased candidate", commonapi.ErrConflict)
			}
			return err
		}
		now, err := DatabaseNow(tx)
		if err != nil {
			return err
		}
		if err := applyTerminalFields(item, ExecutionStatusFailed, fields, now); err != nil {
			return err
		}
		return requireOwnedRow(exact().Updates(fields), executionID, "fail unleased execution")
	})
}
