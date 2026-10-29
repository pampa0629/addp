package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// cleanupCoordinator has one database-backed parent loop. It never invokes an
// Owner, reissues a child, or interprets professional output.
type cleanupCoordinator struct {
	db    *gorm.DB
	issue func(context.Context, *gorm.DB, execution.TaskExecution) error
	audit func(context.Context, *gorm.DB, iam.AuditEvent) error
}

func newCleanupCoordinator(db *gorm.DB) *cleanupCoordinator {
	return &cleanupCoordinator{db: db,
		issue: func(ctx context.Context, tx *gorm.DB, child execution.TaskExecution) error {
			authorizer, err := iam.NewExecutionAuthorizationService(iam.NewRepository(tx))
			if err != nil {
				return err
			}
			_, err = authorizer.IssueCleanup(ctx, uuid.MustParse(child.ExecutionID), iam.AuditMetadata{})
			return err
		},
		audit: func(ctx context.Context, tx *gorm.DB, event iam.AuditEvent) error {
			return iam.NewAuditWriter(iam.NewRepository(tx)).Write(ctx, event)
		},
	}
}

// cleanupIntentDigest excludes server-generated identities and clocks. A retry
// of an accepted ID returns its original immutable request, without issuing new
// authority or refreshing the deadline. Another user action needs another ID.
func cleanupIntentDigest(input events.CleanupRequest, engine, requestedScope string) (string, error) {
	input.RequestID, input.ParentExecutionID, input.Digest, input.Schema = "", "", "", ""
	input.RequestedAt, input.Deadline = time.Time{}, time.Time{}
	// Discovery is a server fact; only the canonical submitted scope is intent.
	input.ExpectedModules = nil
	data, err := json.Marshal(struct {
		Input  events.CleanupRequest
		Engine string
		Scope  string
	}{input, engine, requestedScope})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func (s *cleanupCoordinator) accept(ctx context.Context, input events.CleanupRequest, parent execution.TaskExecution, engine string, scope []string) (*models.CleanupRequestRecord, error) {
	if s == nil || s.db == nil || s.issue == nil || s.audit == nil || input.Validate() != nil || input.RequestedBy == 0 || input.CauseEvent == events.CleanupCauseTenantDeleted || len(engine) > 2048 {
		return nil, commonapi.ErrBadRequest
	}
	requestedScope, err := canonicalCleanupScope(scope)
	if err != nil {
		return nil, err
	}
	intent, err := cleanupIntentDigest(input, engine, requestedScope)
	if err != nil {
		return nil, err
	}
	var accepted models.CleanupRequestRecord
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize only this acceptance identity. The unique indexes remain the
		// final constraint; this lock also makes concurrent same-ID retries read
		// the committed original instead of aborting their transaction.
		if tx.Dialector.Name() == "postgres" {
			key := input.RequestID
			if input.SourceOperationID != "" {
				key = fmt.Sprintf("%d:%s:%s", input.TenantID, input.SourceOperationID, input.Action)
			}
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "cleanup:"+key).Error; err != nil {
				return err
			}
		}
		query := tx.Where("request_id = ?", input.RequestID)
		if input.SourceOperationID != "" {
			query = query.Or("tenant_id = ? AND source_operation_id = ? AND action = ?", input.TenantID, input.SourceOperationID, input.Action)
		}
		var rows []models.CleanupRequestRecord
		if err := query.Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > 0 {
			if len(rows) != 1 || rows[0].TenantID != input.TenantID || rows[0].RequestedBy != input.RequestedBy || rows[0].IntentDigest != intent {
				return commonapi.ErrConflict
			}
			if _, err := rows[0].Input(); err != nil {
				return err
			}
			accepted = rows[0]
			return nil
		}
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		accepted = models.CleanupRequestRecord{RequestID: input.RequestID, TenantID: input.TenantID, RequestedBy: input.RequestedBy, ParentExecutionID: input.ParentExecutionID,
			IntentDigest: intent, RequestedScope: requestedScope, SourceOperationID: input.SourceOperationID, Action: input.Action, Protocol: string(encoded), EngineConfirmation: engine, CreatedAt: input.RequestedAt}
		if _, err := accepted.Input(); err != nil {
			return err
		}
		if err = tx.Create(&accepted).Error; err != nil {
			return err
		}
		children, err := execution.CreateCleanupExecutions(ctx, tx, &parent, input)
		if err != nil {
			return err
		}
		for _, child := range children {
			if err := s.issue(ctx, tx, child); err != nil {
				return err
			}
		}
		event := "cleanup.scan.created"
		if input.Action == events.CleanupActionExecute {
			event = "cleanup.execute.confirmed"
		}
		return s.audit(ctx, tx, cleanupAudit(input, event, map[string]any{"execution_id": input.ParentExecutionID, "expected_modules": input.ExpectedModules, "action": input.Action}))
	})
	return &accepted, err
}

func cleanupAudit(input events.CleanupRequest, name string, details map[string]any) iam.AuditEvent {
	principal, tenant := int64(input.RequestedBy), int64(input.TenantID)
	principalType, contextType := iam.PrincipalTypeUser, iam.ContextTypeTenant
	result, risk := iam.AuditResultSucceeded, iam.AuditRiskMedium
	if name == "cleanup.failed" {
		result, risk = iam.AuditResultFailed, iam.AuditRiskHigh
	}
	if input.Action == events.CleanupActionExecute {
		risk = iam.AuditRiskHigh
	}
	return iam.AuditEvent{Metadata: iam.AuditMetadata{PrincipalID: &principal, PrincipalType: &principalType, ContextType: &contextType, TenantID: &tenant},
		EventName: name, Result: result, RiskLevel: risk, ModuleName: "system", EntityType: "cleanup_task", EntityID: input.RequestID, Details: details}
}

func (s *cleanupCoordinator) tick(ctx context.Context, worker string) error {
	var parents []execution.TaskExecution
	err := s.db.WithContext(ctx).Where("module = ? AND task_type = ? AND source = ? AND execution_boundary = ? AND status IN ?", execution.ModuleSystem, execution.TaskTypeCleanup, execution.ModuleSystem, execution.ExecutionBoundaryBounded, []string{execution.ExecutionStatusPending, execution.ExecutionStatusRunning}).
		Where("execution_id IN (?)", s.db.Model(&models.CleanupRequestRecord{}).Select("parent_execution_id::text")).
		Order("created_at, id").Limit(64).Find(&parents).Error
	if err != nil {
		return err
	}
	var failures []error
	for _, parent := range parents {
		if err := s.advance(ctx, parent.ExecutionID, worker); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// advance takes P only. Child receipt commits never need P, so this SELECT's
// committed snapshot defines the cutoff. A later valid child result is retained
// by its Owner but cannot rewrite this cutoff or turn a timeout into success.
func (s *cleanupCoordinator) advance(ctx context.Context, id, worker string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var parent execution.TaskExecution
		query := tx.Where("execution_id = ? AND module = ? AND task_type = ? AND source = ?", id, execution.ModuleSystem, execution.TaskTypeCleanup, execution.ModuleSystem)
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		if err := query.First(&parent).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		if parent.Status != execution.ExecutionStatusPending && parent.Status != execution.ExecutionStatusRunning {
			return nil
		}
		input, err := execution.CleanupRequestFromExecution(parent)
		if err != nil {
			return err
		}
		var record models.CleanupRequestRecord
		if err := tx.Where("request_id = ? AND parent_execution_id = ?", input.RequestID, id).First(&record).Error; err != nil {
			return err
		}
		privateInput, err := record.Input()
		if err != nil || privateInput.Digest != input.Digest || record.Cutoff != nil {
			return fmt.Errorf("cleanup request/parent mismatch")
		}
		now, err := execution.DatabaseNow(tx)
		if err != nil {
			return err
		}
		var lease execution.Lease
		if parent.Status == execution.ExecutionStatusRunning {
			lease, err = execution.LeaseFromExecution(parent)
			if err != nil {
				return err
			}
			if !parent.LeaseExpiresAt.After(now) {
				if err := execution.RetryExpired(ctx, tx, lease, "cleanup_aggregate"); err != nil {
					return err
				}
				parent.Status = execution.ExecutionStatusPending
			} else if lease.Owner != worker {
				return nil
			}
		}
		if parent.Status == execution.ExecutionStatusPending {
			claimed, ownership, err := execution.ClaimPending(ctx, tx, execution.PendingIdentity{ExecutionID: id, TenantID: parent.TenantID, Module: parent.Module, TaskType: parent.TaskType, Source: parent.Source}, execution.ClaimOptions{Module: parent.Module, TaskType: parent.TaskType, Source: parent.Source, WorkerID: worker, LeaseDuration: 15 * time.Second}, nil)
			if err != nil {
				return err
			}
			parent, lease = *claimed, *ownership
		}
		var children []execution.TaskExecution
		if err := tx.Where("parent_execution_id = ? AND tenant_id = ? AND task_type = ? AND source = ?", id, parent.TenantID, execution.TaskTypeCleanupExecutor, execution.ModuleSystem).Order("module").Find(&children).Error; err != nil {
			return err
		}
		receipts, missing, err := cleanupReceipts(input, children)
		if err != nil {
			return err
		}
		now, err = execution.DatabaseNow(tx)
		if err != nil {
			return err
		}
		if len(missing) > 0 && now.Before(input.Deadline) {
			return execution.RenewLease(ctx, tx, lease, 15*time.Second)
		}
		status := execution.ExecutionStatusSuccess
		if len(missing) > 0 {
			status = execution.ExecutionStatusTimeout
		} else {
			for _, receipt := range receipts {
				if receipt.Status != events.CleanupResultSuccess && receipt.Status != events.CleanupResultSkipped {
					status = execution.ExecutionStatusFailed
				}
			}
		}
		cutoff := models.CleanupCutoff{ObservedAt: now, Status: status, Receipts: receipts, Missing: missing}
		data, err := json.Marshal(cutoff)
		if err != nil {
			return err
		}
		updated := tx.Model(&models.CleanupRequestRecord{}).Where("request_id = ? AND cutoff IS NULL", record.RequestID).Update("cutoff", string(data))
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return commonapi.ErrConflict
		}
		fields := map[string]interface{}{"progress": 100}
		if status != execution.ExecutionStatusSuccess {
			fields["error_details"] = commonmodels.JSONMap{"code": "cleanup_receipts_incomplete_or_failed"}
		}
		if err := execution.CompleteWithLease(ctx, tx, lease, status, fields); err != nil {
			return err
		}
		event := "cleanup.completed"
		if status != execution.ExecutionStatusSuccess {
			event = "cleanup.failed"
		}
		return s.audit(ctx, tx, cleanupAudit(input, event, map[string]any{"execution_id": id, "status": status, "receipt_count": len(receipts), "missing_modules": missing}))
	})
}

func cleanupReceipts(input events.CleanupRequest, children []execution.TaskExecution) ([]execution.CleanupReceipt, []string, error) {
	receipts := []execution.CleanupReceipt{}
	byModule := map[string]execution.TaskExecution{}
	for _, child := range children {
		bound, err := execution.CleanupRequestFromExecution(child)
		if err != nil || bound.Digest != input.Digest {
			return nil, nil, fmt.Errorf("invalid cleanup child binding")
		}
		if _, exists := byModule[child.Module]; exists {
			return nil, nil, fmt.Errorf("duplicate cleanup Owner")
		}
		byModule[child.Module] = child
	}
	missing := []string{}
	for _, owner := range input.ExpectedModules {
		child, ok := byModule[owner]
		if !ok {
			return nil, nil, fmt.Errorf("accepted cleanup child is missing")
		}
		receipt, err := execution.ReadCleanupReceipt(child)
		if err != nil {
			return nil, nil, err
		}
		if receipt == nil {
			missing = append(missing, owner)
		} else {
			receipts = append(receipts, *receipt)
		}
	}
	return receipts, missing, nil
}

type cleanupSnapshot struct {
	Record     models.CleanupRequestRecord
	Input      events.CleanupRequest
	Parent     execution.TaskExecution
	Children   []execution.TaskExecution
	ObservedAt time.Time
}

// snapshot is an internal fact read. User-facing callers must authorize the
// parent before calling it and filter children by current Owner ReadScopes.
func (s *cleanupCoordinator) snapshot(ctx context.Context, requestID string, tenant uint) (*cleanupSnapshot, error) {
	result := &cleanupSnapshot{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("request_id = ? AND tenant_id = ?", requestID, tenant).First(&result.Record).Error; err != nil {
			return err
		}
		var err error
		result.Input, err = result.Record.Input()
		if err != nil {
			return err
		}
		if err := tx.Where("execution_id = ? AND tenant_id = ?", result.Record.ParentExecutionID, tenant).First(&result.Parent).Error; err != nil {
			return err
		}
		bound, err := execution.CleanupRequestFromExecution(result.Parent)
		if err != nil || bound.Digest != result.Input.Digest {
			return fmt.Errorf("invalid cleanup parent binding")
		}
		if err := tx.Where("parent_execution_id = ? AND tenant_id = ? AND task_type = ?", result.Parent.ExecutionID, tenant, execution.TaskTypeCleanupExecutor).Order("module").Find(&result.Children).Error; err != nil {
			return err
		}
		if _, _, err := cleanupReceipts(result.Input, result.Children); err != nil {
			return err
		}
		result.ObservedAt, err = execution.DatabaseNow(tx)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}
