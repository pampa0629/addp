package execution

import (
	"context"
	"fmt"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type leaseContextKey struct{}

func ContextWithLease(ctx context.Context, lease Lease) context.Context {
	return context.WithValue(ctx, leaseContextKey{}, lease)
}

func LeaseFromContext(ctx context.Context) (Lease, bool) {
	if ctx == nil {
		return Lease{}, false
	}
	lease, ok := ctx.Value(leaseContextKey{}).(Lease)
	return lease, ok
}

// ClaimOptions identifies one bounded execution queue. Owner repositories may
// compose this primitive with owner-task updates in the same transaction.
type ClaimOptions struct {
	Module               string
	TaskType             string
	Source               string
	WorkerID             string
	Now                  time.Time
	LeaseDuration        time.Duration
	RequireAuthorization bool
}

// Lease identifies one non-reusable bounded execution attempt.
type Lease struct {
	ExecutionID string
	TenantID    int
	Attempt     int
	Token       string
	Owner       string
}

type ExpiredOptions struct {
	Module   string
	TaskType string
	Source   string
	Now      time.Time
	Limit    int
}

func ClaimNext(ctx context.Context, tx *gorm.DB, options ClaimOptions) (*TaskExecution, *Lease, error) {
	if tx == nil {
		return nil, nil, fmt.Errorf("execution claim database is required")
	}
	if options.Module == "" || options.TaskType == "" || options.WorkerID == "" {
		return nil, nil, fmt.Errorf("execution claim module, task type and worker ID are required")
	}
	if options.LeaseDuration <= 0 {
		return nil, nil, fmt.Errorf("execution claim lease duration must be positive")
	}
	now := options.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}

	var item TaskExecution
	query := tx.WithContext(ctx).
		Where("module = ? AND task_type = ? AND execution_boundary = ? AND status = ?", options.Module, options.TaskType, ExecutionBoundaryBounded, ExecutionStatusPending).
		Order("created_at ASC, id ASC").Limit(1)
	if options.Source != "" {
		query = query.Where("source = ?", options.Source)
	}
	if options.RequireAuthorization {
		query = query.Where("execution_authorization_id IS NOT NULL")
	}
	if tx.Dialector.Name() == "postgres" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
	}
	result := query.Find(&item)
	if result.Error != nil {
		return nil, nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil, nil
	}

	token := uuid.NewString()
	expiresAt := now.Add(options.LeaseDuration)
	update := tx.WithContext(ctx).Model(&TaskExecution{}).
		Where("execution_id = ? AND tenant_id = ? AND status = ?", item.ExecutionID, item.TenantID, ExecutionStatusPending).
		Updates(map[string]interface{}{
			"status": ExecutionStatusRunning, "started_at": now, "updated_at": now,
			"lease_owner": options.WorkerID, "lease_token": token, "lease_expires_at": expiresAt,
			"attempt": gorm.Expr("attempt + 1"),
		})
	if update.Error != nil {
		return nil, nil, update.Error
	}
	if update.RowsAffected != 1 {
		return nil, nil, fmt.Errorf("%w: execution %s was claimed by another worker", commonapi.ErrConflict, item.ExecutionID)
	}

	item.Status = ExecutionStatusRunning
	item.StartedAt = &now
	item.UpdatedAt = now
	item.Attempt++
	item.LeaseOwner = &options.WorkerID
	item.LeaseToken = &token
	item.LeaseExpiresAt = &expiresAt
	lease := &Lease{ExecutionID: item.ExecutionID, TenantID: item.TenantID, Attempt: item.Attempt, Token: token, Owner: options.WorkerID}
	return &item, lease, nil
}

func RenewLease(ctx context.Context, tx *gorm.DB, lease Lease, expiresAt time.Time) error {
	result := ownedActiveExecution(tx.WithContext(ctx), lease, time.Now().UTC()).
		Updates(map[string]interface{}{"lease_expires_at": expiresAt.UTC(), "updated_at": time.Now().UTC()})
	return requireOwnedRow(result, lease.ExecutionID, "renew")
}

// AttemptIsTerminal reports whether the exact claimed attempt has already
// reached a terminal state. Workers use this to distinguish the normal race
// between terminal completion and the final heartbeat from genuine lease loss.
func AttemptIsTerminal(ctx context.Context, tx *gorm.DB, lease Lease) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("execution lease database is required")
	}
	var count int64
	err := tx.WithContext(ctx).Model(&TaskExecution{}).
		Where("execution_id = ? AND tenant_id = ? AND attempt = ? AND status IN ?", lease.ExecutionID, lease.TenantID, lease.Attempt, []string{
			ExecutionStatusSuccess,
			ExecutionStatusFailed,
			ExecutionStatusTimeout,
			ExecutionStatusCancelled,
		}).
		Count(&count).Error
	return count == 1, err
}

func LeaseFromExecution(item TaskExecution) (Lease, error) {
	if item.LeaseToken == nil || *item.LeaseToken == "" || item.LeaseOwner == nil || *item.LeaseOwner == "" {
		return Lease{}, fmt.Errorf("execution %s has no active lease identity", item.ExecutionID)
	}
	return Lease{ExecutionID: item.ExecutionID, TenantID: item.TenantID, Attempt: item.Attempt, Token: *item.LeaseToken, Owner: *item.LeaseOwner}, nil
}

// FindExpiredForUpdate locks expired bounded attempts inside the caller's
// transaction. The owner decides whether each execution is safe to retry.
func FindExpiredForUpdate(ctx context.Context, tx *gorm.DB, options ExpiredOptions) ([]TaskExecution, error) {
	if tx == nil || options.Module == "" || options.TaskType == "" {
		return nil, fmt.Errorf("expired execution database, module and task type are required")
	}
	now := options.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	limit := options.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	query := tx.WithContext(ctx).
		Where("module = ? AND task_type = ? AND execution_boundary = ? AND status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at < ?", options.Module, options.TaskType, ExecutionBoundaryBounded, ExecutionStatusRunning, now).
		Order("lease_expires_at ASC, id ASC").Limit(limit)
	if options.Source != "" {
		query = query.Where("source = ?", options.Source)
	}
	if tx.Dialector.Name() == "postgres" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
	}
	var items []TaskExecution
	if err := query.Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func RetryExpired(ctx context.Context, tx *gorm.DB, lease Lease, now time.Time, currentStep string) error {
	fields := map[string]interface{}{
		"status": ExecutionStatusPending, "lease_owner": nil, "lease_token": nil,
		"lease_expires_at": nil, "updated_at": now.UTC(), "current_step": currentStep,
	}
	result := ownedExpiredExecution(tx.WithContext(ctx), lease, now).Updates(fields)
	return requireOwnedRow(result, lease.ExecutionID, "retry expired attempt")
}

func FailExpired(ctx context.Context, tx *gorm.DB, lease Lease, now time.Time, fields map[string]interface{}) error {
	if fields == nil {
		fields = map[string]interface{}{}
	}
	fields["status"] = ExecutionStatusFailed
	fields["completed_at"] = now.UTC()
	fields["updated_at"] = now.UTC()
	fields["lease_owner"] = nil
	fields["lease_token"] = nil
	fields["lease_expires_at"] = nil
	result := ownedExpiredExecution(tx.WithContext(ctx), lease, now).Updates(fields)
	return requireOwnedRow(result, lease.ExecutionID, "fail expired attempt")
}

func CompleteWithLease(ctx context.Context, tx *gorm.DB, lease Lease, status string, completedAt time.Time, fields map[string]interface{}) error {
	switch status {
	case ExecutionStatusSuccess, ExecutionStatusFailed, ExecutionStatusTimeout, ExecutionStatusCancelled:
	default:
		return fmt.Errorf("execution terminal status %q is invalid", status)
	}
	if fields == nil {
		fields = map[string]interface{}{}
	}
	for _, protected := range []string{"execution_id", "tenant_id", "attempt", "lease_token", "lease_owner", "lease_expires_at", "status", "completed_at"} {
		if _, exists := fields[protected]; exists {
			return fmt.Errorf("execution completion cannot replace %s", protected)
		}
	}
	fields["status"] = status
	fields["completed_at"] = completedAt.UTC()
	fields["updated_at"] = completedAt.UTC()
	fields["lease_owner"] = nil
	fields["lease_token"] = nil
	fields["lease_expires_at"] = nil
	result := ownedActiveExecution(tx.WithContext(ctx), lease, time.Now().UTC()).Updates(fields)
	return requireOwnedRow(result, lease.ExecutionID, "complete")
}

// UpdateWithLease updates execution fields only while the supplied attempt is
// still the current legal owner. Protected ownership fields cannot be replaced.
func UpdateWithLease(ctx context.Context, tx *gorm.DB, lease Lease, fields map[string]interface{}) error {
	if fields == nil {
		return fmt.Errorf("execution lease update fields are required")
	}
	for _, protected := range []string{"execution_id", "tenant_id", "attempt", "lease_token", "lease_owner", "lease_expires_at", "status", "completed_at"} {
		if _, exists := fields[protected]; exists {
			return fmt.Errorf("execution lease update cannot replace %s", protected)
		}
	}
	if _, exists := fields["updated_at"]; !exists {
		fields["updated_at"] = time.Now().UTC()
	}
	result := ownedActiveExecution(tx.WithContext(ctx), lease, time.Now().UTC()).Updates(fields)
	return requireOwnedRow(result, lease.ExecutionID, "update")
}

func ownedExecution(db *gorm.DB, lease Lease) *gorm.DB {
	return db.Model(&TaskExecution{}).
		Where("execution_id = ? AND tenant_id = ? AND status = ? AND attempt = ? AND lease_token = ?", lease.ExecutionID, lease.TenantID, ExecutionStatusRunning, lease.Attempt, lease.Token)
}

func ownedActiveExecution(db *gorm.DB, lease Lease, now time.Time) *gorm.DB {
	return ownedExecution(db, lease).
		Where("lease_expires_at IS NOT NULL AND lease_expires_at >= ?", now.UTC())
}

func ownedExpiredExecution(db *gorm.DB, lease Lease, now time.Time) *gorm.DB {
	return ownedExecution(db, lease).Where("lease_expires_at IS NOT NULL AND lease_expires_at < ?", now.UTC())
}

func requireOwnedRow(result *gorm.DB, executionID, operation string) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: execution %s lease no longer owns %s", commonapi.ErrConflict, executionID, operation)
	}
	return nil
}
