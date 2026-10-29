package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/models"
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
	TaskTypes            []string
	Source               string
	WorkerID             string
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
	Module    string
	TaskType  string
	TaskTypes []string
	Source    string
	Limit     int
}

// PendingIdentity binds an exact candidate before an owner acquires other locks.
type PendingIdentity struct {
	ExecutionID string
	TenantID    int
	Module      string
	TaskType    string
	Source      string
}

func ClaimNext(ctx context.Context, db *gorm.DB, options ClaimOptions) (*TaskExecution, *Lease, error) {
	return claim(ctx, db, options, nil, nil)
}

// ClaimPending must be composed after the owner has acquired its required locks.
// notAfter is an optional exclusive admission deadline, not a caller clock.
func ClaimPending(ctx context.Context, db *gorm.DB, identity PendingIdentity, options ClaimOptions, notAfter *time.Time) (*TaskExecution, *Lease, error) {
	if identity.ExecutionID == "" || identity.TenantID <= 0 || identity.Module != options.Module || identity.TaskType == "" || identity.Source == "" {
		return nil, nil, fmt.Errorf("execution claim requires an exact pending identity")
	}
	return claim(ctx, db, options, &identity, notAfter)
}

func claim(ctx context.Context, db *gorm.DB, options ClaimOptions, identity *PendingIdentity, notAfter *time.Time) (*TaskExecution, *Lease, error) {
	if db == nil || options.Module == "" || options.WorkerID == "" || options.LeaseDuration <= 0 {
		return nil, nil, fmt.Errorf("execution claim database, module, worker and positive lease duration are required")
	}
	if err := validateTaskTypeSelector(options.TaskType, options.TaskTypes); err != nil {
		return nil, nil, err
	}
	var item *TaskExecution
	var lease *Lease
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Session(&gorm.Session{}).Where("module = ? AND execution_boundary = ? AND status = ?", options.Module, ExecutionBoundaryBounded, ExecutionStatusPending)
		query = applyTaskTypeSelector(query, options.TaskType, options.TaskTypes)
		if options.Source != "" {
			query = query.Where("source = ?", options.Source)
		}
		if options.RequireAuthorization {
			query = query.Where("execution_authorization_id IS NOT NULL")
		}
		if identity != nil {
			query = query.Where("execution_id = ? AND tenant_id = ? AND task_type = ? AND source = ?", identity.ExecutionID, identity.TenantID, identity.TaskType, identity.Source)
		}
		if tx.Dialector.Name() == "postgres" {
			locking := clause.Locking{Strength: "UPDATE"}
			if identity == nil {
				locking.Options = "SKIP LOCKED"
			}
			query = query.Clauses(locking)
		}
		var candidate TaskExecution
		result := query.Order("created_at ASC, id ASC").Limit(1).Find(&candidate)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			if identity != nil {
				return fmt.Errorf("%w: pending execution changed", commonapi.ErrConflict)
			}
			return nil
		}
		if candidate.StartedAt != nil || candidate.CompletedAt != nil || candidate.ExecutionTimeMs != nil || candidate.LeaseToken != nil || candidate.LeaseOwner != nil || candidate.LeaseExpiresAt != nil {
			return fmt.Errorf("%w: pending execution has running facts", commonapi.ErrConflict)
		}
		now, err := DatabaseNow(tx)
		if err != nil {
			return err
		}
		if notAfter != nil && !now.Before(*notAfter) {
			return fmt.Errorf("%w: execution admission closed", commonapi.ErrConflict)
		}
		token := uuid.NewString()
		expiresAt := now.Add(options.LeaseDuration)
		update := tx.Session(&gorm.Session{}).Model(&TaskExecution{}).Where("execution_id = ? AND tenant_id = ? AND status = ?", candidate.ExecutionID, candidate.TenantID, ExecutionStatusPending)
		if notAfter != nil && tx.Dialector.Name() == "postgres" {
			update = update.Where("clock_timestamp() < ?", *notAfter)
		}
		result = update.Updates(map[string]interface{}{"status": ExecutionStatusRunning, "started_at": now, "updated_at": now, "attempt": gorm.Expr("attempt + 1"), "lease_owner": options.WorkerID, "lease_token": token, "lease_expires_at": expiresAt})
		if err := requireOwnedRow(result, candidate.ExecutionID, "claim"); err != nil {
			return err
		}
		candidate.Status = ExecutionStatusRunning
		candidate.StartedAt = &now
		candidate.UpdatedAt = now
		candidate.Attempt++
		candidate.LeaseOwner = &options.WorkerID
		candidate.LeaseToken = &token
		candidate.LeaseExpiresAt = &expiresAt
		item = &candidate
		lease = &Lease{ExecutionID: candidate.ExecutionID, TenantID: candidate.TenantID, Attempt: candidate.Attempt, Token: token, Owner: options.WorkerID}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return item, lease, nil
}

// DatabaseNow is the shared lifecycle clock. PostgreSQL never trusts worker time.
func DatabaseNow(db *gorm.DB) (time.Time, error) {
	if db == nil {
		return time.Time{}, fmt.Errorf("execution database is required")
	}
	if db.Dialector.Name() != "postgres" {
		return time.Now().UTC(), nil
	}
	var now time.Time
	err := db.Session(&gorm.Session{NewDB: true}).Raw("SELECT clock_timestamp()").Scan(&now).Error
	return now.UTC(), err
}

func RenewLease(ctx context.Context, db *gorm.DB, lease Lease, duration time.Duration) error {
	if duration <= 0 {
		return fmt.Errorf("execution lease duration must be positive")
	}
	return withLockedAttempt(ctx, db, lease, false, func(tx *gorm.DB, item TaskExecution, now time.Time) error {
		return requireOwnedRow(ownedActiveExecution(tx, lease, now).Updates(map[string]interface{}{"lease_expires_at": now.Add(duration), "updated_at": now}), lease.ExecutionID, "renew")
	})
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
	if tx == nil || options.Module == "" {
		return nil, fmt.Errorf("expired execution database and module are required")
	}
	if err := validateTaskTypeSelector(options.TaskType, options.TaskTypes); err != nil {
		return nil, fmt.Errorf("expired execution: %w", err)
	}
	now, err := DatabaseNow(tx.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	limit := options.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	query := tx.WithContext(ctx).
		Where("module = ? AND execution_boundary = ? AND status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?", options.Module, ExecutionBoundaryBounded, ExecutionStatusRunning, now).
		Order("lease_expires_at ASC, id ASC").Limit(limit)
	query = applyTaskTypeSelector(query, options.TaskType, options.TaskTypes)
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

func validateTaskTypeSelector(taskType string, taskTypes []string) error {
	if taskType != "" && len(taskTypes) > 0 {
		return fmt.Errorf("task type and task types cannot both be set")
	}
	if taskType == "" && len(taskTypes) == 0 {
		return fmt.Errorf("task type selector is required")
	}
	for _, candidate := range taskTypes {
		if candidate == "" {
			return fmt.Errorf("task types cannot contain an empty value")
		}
	}
	return nil
}

func applyTaskTypeSelector(query *gorm.DB, taskType string, taskTypes []string) *gorm.DB {
	if taskType != "" {
		return query.Where("task_type = ?", taskType)
	}
	return query.Where("task_type IN ?", taskTypes)
}

func RetryExpired(ctx context.Context, db *gorm.DB, lease Lease, currentStep string) error {
	return withLockedAttempt(ctx, db, lease, true, func(tx *gorm.DB, item TaskExecution, now time.Time) error {
		fields := map[string]interface{}{"status": ExecutionStatusPending, "lease_owner": nil, "lease_token": nil, "lease_expires_at": nil, "started_at": nil, "completed_at": nil, "execution_time_ms": nil, "updated_at": now, "current_step": currentStep}
		if item.Source == ModuleOrchestrator && item.ParentExecutionID != nil {
			fields["execution_authorization_id"] = nil
			fields["authorization_expires_at"] = nil
		}
		return requireOwnedRow(ownedExpiredExecution(tx, lease, now).Updates(fields), lease.ExecutionID, "retry expired attempt")
	})
}

func FailExpired(ctx context.Context, db *gorm.DB, lease Lease, fields map[string]interface{}) error {
	return finishAttempt(ctx, db, lease, ExecutionStatusFailed, fields, true)
}

func CompleteWithLease(ctx context.Context, db *gorm.DB, lease Lease, status string, fields map[string]interface{}) error {
	return finishAttempt(ctx, db, lease, status, fields, false)
}

func finishAttempt(ctx context.Context, db *gorm.DB, lease Lease, status string, input map[string]interface{}, expired bool) error {
	switch status {
	case ExecutionStatusSuccess, ExecutionStatusFailed, ExecutionStatusTimeout, ExecutionStatusCancelled:
	default:
		return fmt.Errorf("execution terminal status %q is invalid", status)
	}
	fields, err := copyExecutionFields(input)
	if err != nil {
		return err
	}
	return withLockedAttempt(ctx, db, lease, expired, func(tx *gorm.DB, item TaskExecution, now time.Time) error {
		if err := applyTerminalFields(item, status, fields, now); err != nil {
			return err
		}
		query := ownedActiveExecution(tx, lease, now)
		if expired {
			query = ownedExpiredExecution(tx, lease, now)
		}
		return requireOwnedRow(query.Updates(fields), lease.ExecutionID, "complete")
	})
}

func applyTerminalFields(item TaskExecution, status string, fields map[string]interface{}, now time.Time) error {
	if err := preservePrivateMetadata(fields, item.Metadata); err != nil {
		return err
	}
	fields["status"], fields["completed_at"], fields["updated_at"] = status, now, now
	fields["lease_owner"], fields["lease_token"], fields["lease_expires_at"] = nil, nil, nil
	fields["execution_time_ms"] = nil
	if item.StartedAt != nil {
		if now.Before(*item.StartedAt) {
			return fmt.Errorf("%w: execution clock precedes attempt start", commonapi.ErrConflict)
		}
		fields["execution_time_ms"] = now.Sub(*item.StartedAt).Milliseconds()
	}
	return nil
}

// UpdateWithLease accepts only progress, diagnostics and owner result fields.
func UpdateWithLease(ctx context.Context, db *gorm.DB, lease Lease, input map[string]interface{}) error {
	fields, err := copyExecutionFields(input)
	if err != nil {
		return err
	}
	return withLockedAttempt(ctx, db, lease, false, func(tx *gorm.DB, item TaskExecution, now time.Time) error {
		if err := preservePrivateMetadata(fields, item.Metadata); err != nil {
			return err
		}
		fields["updated_at"] = now
		return requireOwnedRow(ownedActiveExecution(tx, lease, now).Updates(fields), lease.ExecutionID, "update")
	})
}

func copyExecutionFields(input map[string]interface{}) (map[string]interface{}, error) {
	fields := make(map[string]interface{}, len(input)+8)
	for key, value := range input {
		switch key {
		case "progress", "current_step", "rows_affected", "records_read", "records_written", "bytes_read", "bytes_written", "error_details", "metadata":
			fields[key] = value
		default:
			return nil, fmt.Errorf("execution update cannot replace %s", key)
		}
	}
	return fields, nil
}

func preservePrivateMetadata(fields map[string]interface{}, prior models.JSONMap) error {
	value, exists := fields["metadata"]
	if !exists {
		return nil
	}
	next := models.JSONMap{}
	if value != nil {
		switch typed := value.(type) {
		case models.JSONMap:
			for k, v := range typed {
				next[k] = v
			}
		case map[string]interface{}:
			for k, v := range typed {
				next[k] = v
			}
		default:
			return fmt.Errorf("execution metadata must be an object")
		}
	}
	if _, exists := next["cleanup_receipt"]; exists {
		return fmt.Errorf("cleanup receipt requires typed completion")
	}
	if _, exists := prior["cleanup_receipt"]; exists {
		return fmt.Errorf("cleanup completion receipt is immutable")
	}
	if origin, exists := next[SchemaChangeScanOriginKey]; exists {
		old, recorded := prior[SchemaChangeScanOriginKey]
		oldBytes, oldErr := json.Marshal(old)
		newBytes, newErr := json.Marshal(origin)
		if !recorded || oldErr != nil || newErr != nil || !bytes.Equal(oldBytes, newBytes) {
			return fmt.Errorf("schema change scan origin requires typed publication")
		}
	} else if origin, exists := prior[SchemaChangeScanOriginKey]; exists {
		next[SchemaChangeScanOriginKey] = origin
	}
	if trace, ok := next[ObservabilityKey]; ok {
		if _, err := traceValue(trace, "latest"); err != nil {
			oldBytes, _ := json.Marshal(prior[ObservabilityKey])
			newBytes, _ := json.Marshal(trace)
			if !bytes.Equal(oldBytes, newBytes) {
				return err
			}
		}
	} else if trace, ok := prior[ObservabilityKey]; ok {
		next[ObservabilityKey] = trace
	}
	fields["metadata"] = next
	return nil
}

// LockActiveLease acquires E before owner rows. Callers must finish with a fenced
// mutation after their domain locks and writes, in the same outer transaction.
func LockActiveLease(ctx context.Context, tx *gorm.DB, lease Lease) (*TaskExecution, error) {
	item, _, err := lockAttempt(ctx, tx, lease, false)
	return item, err
}

func withLockedAttempt(ctx context.Context, db *gorm.DB, lease Lease, expired bool, write func(*gorm.DB, TaskExecution, time.Time) error) error {
	if db == nil {
		return fmt.Errorf("execution lease database is required")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, now, err := lockAttempt(ctx, tx, lease, expired)
		if err != nil {
			return err
		}
		return write(tx.Session(&gorm.Session{}), *item, now)
	})
}

func lockAttempt(ctx context.Context, tx *gorm.DB, lease Lease, expired bool) (*TaskExecution, time.Time, error) {
	if tx == nil || lease.ExecutionID == "" || lease.TenantID <= 0 || lease.Attempt <= 0 || lease.Owner == "" || lease.Token == "" {
		return nil, time.Time{}, fmt.Errorf("execution requires a complete bounded lease")
	}
	query := ownedExecution(tx.WithContext(ctx).Session(&gorm.Session{}), lease)
	if tx.Dialector.Name() == "postgres" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var item TaskExecution
	if err := query.First(&item).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			err = fmt.Errorf("%w: execution lease is no longer current", commonapi.ErrConflict)
		}
		return nil, time.Time{}, err
	}
	now, err := DatabaseNow(tx)
	if err != nil {
		return nil, time.Time{}, err
	}
	active := item.LeaseExpiresAt != nil && item.LeaseExpiresAt.After(now)
	if item.LeaseExpiresAt == nil || active == expired {
		return nil, time.Time{}, fmt.Errorf("%w: execution lease validity changed", commonapi.ErrConflict)
	}
	return &item, now, nil
}

func ownedExecution(db *gorm.DB, lease Lease) *gorm.DB {
	return db.Session(&gorm.Session{}).Model(&TaskExecution{}).Where("execution_id = ? AND tenant_id = ? AND execution_boundary = ? AND status = ? AND attempt = ? AND lease_token = ? AND lease_owner = ?", lease.ExecutionID, lease.TenantID, ExecutionBoundaryBounded, ExecutionStatusRunning, lease.Attempt, lease.Token, lease.Owner)
}

func ownedActiveExecution(db *gorm.DB, lease Lease, now time.Time) *gorm.DB {
	query := ownedExecution(db, lease)
	if db.Dialector.Name() == "postgres" {
		return query.Where("lease_expires_at > clock_timestamp()")
	}
	return query.Where("lease_expires_at > ?", now.UTC())
}

func ownedExpiredExecution(db *gorm.DB, lease Lease, now time.Time) *gorm.DB {
	query := ownedExecution(db, lease)
	if db.Dialector.Name() == "postgres" {
		return query.Where("lease_expires_at <= clock_timestamp()")
	}
	return query.Where("lease_expires_at <= ?", now.UTC())
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
