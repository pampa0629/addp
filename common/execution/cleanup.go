package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/events"
	"github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const cleanupInputKey = "cleanup_request"
const CleanupReceiptSchema = "addp.cleanup-receipt/v1"
const cleanupRejectionSchema = "addp.cleanup-rejection/v1"

// CleanupRequestFromExecution decodes frozen protocol input, not user permission.
func CleanupRequestFromExecution(e TaskExecution) (events.CleanupRequest, error) {
	var input events.CleanupRequest
	value, ok := e.ExecutionConfig[cleanupInputKey].(string)
	if !ok || events.CleanupStrictDecode([]byte(value), &input) != nil || input.Validate() != nil {
		return input, fmt.Errorf("invalid cleanup execution input")
	}
	id, uuidErr := uuid.Parse(e.ExecutionID)
	if uuidErr != nil || id == uuid.Nil || id.String() != e.ExecutionID {
		return input, fmt.Errorf("invalid cleanup execution ID")
	}
	if e.TenantID != int(input.TenantID) || e.Source != ModuleSystem || e.ExecutionBoundary != ExecutionBoundaryBounded || e.SourceTaskID != nil || e.TriggerType != input.TriggerType {
		return input, fmt.Errorf("cleanup execution identity mismatch")
	}
	if e.TaskType == TaskTypeCleanup {
		if e.Module != ModuleSystem || e.ExecutionID != input.ParentExecutionID || e.ParentExecutionID != nil {
			return input, fmt.Errorf("invalid cleanup parent")
		}
	} else if e.TaskType == TaskTypeCleanupExecutor {
		if e.ParentExecutionID == nil || *e.ParentExecutionID != input.ParentExecutionID || !events.CleanupModuleValid(e.Module) || !events.CleanupExpectedForModule(input.ExpectedModules, e.Module) {
			return input, fmt.Errorf("invalid cleanup child")
		}
	} else {
		return input, fmt.Errorf("invalid cleanup execution type")
	}
	if input.RequestedBy > 0 && (e.TriggeredBy == nil || *e.TriggeredBy != int(input.RequestedBy)) || input.RequestedBy == 0 && e.TriggeredBy != nil {
		return input, fmt.Errorf("cleanup actor mismatch")
	}
	if input.RequestedBy > 0 && (e.ActorPrincipalID == nil || *e.ActorPrincipalID != int64(input.RequestedBy) || e.ActorTenantMembershipID == nil || *e.ActorTenantMembershipID <= 0 || e.IssuedAuthorizationVersion == nil || *e.IssuedAuthorizationVersion <= 0) {
		return input, fmt.Errorf("cleanup user origin missing")
	}
	if input.RequestedBy == 0 && (e.ActorPrincipalID != nil || e.ActorTenantMembershipID != nil || e.IssuedAuthorizationVersion != nil) {
		return input, fmt.Errorf("cleanup event origin is inconsistent")
	}
	return input, nil
}

// CreateCleanupExecutions is the single System precreation protocol. It must
// share the caller transaction with the private request and acceptance audit.
func CreateCleanupExecutions(ctx context.Context, db *gorm.DB, parent *TaskExecution, input events.CleanupRequest) ([]TaskExecution, error) {
	if db == nil || parent == nil || input.Validate() != nil || parent.ExecutionID != input.ParentExecutionID || parent.Module != ModuleSystem || parent.TaskType != TaskTypeCleanup || parent.ParentExecutionID != nil {
		return nil, fmt.Errorf("invalid cleanup precreation")
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	if parent.ExecutionConfig == nil {
		parent.ExecutionConfig = models.JSONMap{}
	}
	parent.ExecutionConfig[cleanupInputKey] = string(encoded)
	if _, err = CleanupRequestFromExecution(*parent); err != nil {
		return nil, err
	}
	if _, err = DisplayLanguage(parent); err != nil {
		return nil, err
	}
	children := make([]TaskExecution, 0, len(input.ExpectedModules))
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := CreatePending(ctx, tx, parent); err != nil {
			return err
		}
		for _, owner := range input.ExpectedModules {
			child := TaskExecution{ExecutionID: uuid.NewString(), TenantID: parent.TenantID, Module: owner, TaskType: TaskTypeCleanupExecutor, Source: ModuleSystem, Status: ExecutionStatusPending, ExecutionBoundary: ExecutionBoundaryBounded, ParentExecutionID: &parent.ExecutionID, TriggerType: parent.TriggerType, MaxAttempts: 1, ExecutionConfig: models.JSONMap{cleanupInputKey: string(encoded), "display_language": parent.ExecutionConfig["display_language"]}}
			if parent.TriggeredBy != nil {
				if err := CopyActorOrigin(&child, parent); err != nil {
					return err
				}
			}
			if _, err := CleanupRequestFromExecution(child); err != nil {
				return err
			}
			if err := CreatePending(ctx, tx, &child); err != nil {
				return err
			}
			children = append(children, child)
		}
		return nil
	})
	return children, err
}

func cleanupUnstarted(e TaskExecution) bool {
	_, receipt := e.Metadata["cleanup_receipt"]
	return e.Attempt == 0 && e.StartedAt == nil && e.ExecutionTimeMs == nil && e.LeaseOwner == nil && e.LeaseToken == nil && e.LeaseExpiresAt == nil && e.Progress == 0 && !receipt
}

func cleanupBinding(parent, child TaskExecution) (events.CleanupRequest, error) {
	p, err := CleanupRequestFromExecution(parent)
	if err != nil {
		return p, err
	}
	c, err := CleanupRequestFromExecution(child)
	if err != nil {
		return p, err
	}
	if parent.TaskType != TaskTypeCleanup || child.TaskType != TaskTypeCleanupExecutor || p.Digest != c.Digest {
		return p, fmt.Errorf("cleanup parent/child binding mismatch")
	}
	if p.RequestedBy > 0 && (*parent.ActorTenantMembershipID != *child.ActorTenantMembershipID || *parent.IssuedAuthorizationVersion != *child.IssuedAuthorizationVersion) {
		return p, fmt.Errorf("cleanup parent/child origin mismatch")
	}
	return p, nil
}

func terminalCleanupParent(e TaskExecution) bool {
	switch e.Status {
	case ExecutionStatusSuccess, ExecutionStatusFailed, ExecutionStatusTimeout, ExecutionStatusCancelled:
		return e.CompletedAt != nil
	default:
		return false
	}
}

// ClaimCleanupChild serializes parent closure and exact first claim in P -> C
// order. prepare may take Owner locks and verify domain admission; it must not
// call external services or create effects. No runner may start before commit.
// nil lease means temporary waiting or a committed unstarted rejection.
func ClaimCleanupChild(ctx context.Context, db *gorm.DB, identity PendingIdentity, options ClaimOptions, prepare func(*gorm.DB, TaskExecution, events.CleanupRequest) error) (*TaskExecution, *Lease, error) {
	if db == nil || !events.CleanupModuleValid(identity.Module) || identity.Module != options.Module || identity.TaskType != TaskTypeCleanupExecutor || options.TaskType != TaskTypeCleanupExecutor || identity.Source != ModuleSystem || options.Source != ModuleSystem {
		return nil, nil, fmt.Errorf("invalid cleanup owner claim")
	}
	var result *TaskExecution
	var lease *Lease
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var candidate TaskExecution
		if err := tx.Where("execution_id = ? AND tenant_id = ? AND module = ? AND task_type = ? AND source = ?", identity.ExecutionID, identity.TenantID, identity.Module, identity.TaskType, identity.Source).First(&candidate).Error; err != nil {
			return err
		}
		if candidate.ParentExecutionID == nil {
			return fmt.Errorf("cleanup parent is missing")
		}
		var parent, child TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id = ? AND tenant_id = ?", *candidate.ParentExecutionID, identity.TenantID).First(&parent).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id = ? AND tenant_id = ?", identity.ExecutionID, identity.TenantID).First(&child).Error; err != nil {
			return err
		}
		input, err := cleanupBinding(parent, child)
		if err != nil {
			return err
		}
		if child.Module != identity.Module || child.Status != ExecutionStatusPending || !cleanupUnstarted(child) || child.CompletedAt != nil {
			return commonapi.ErrConflict
		}
		now, err := DatabaseNow(tx)
		if err != nil {
			return err
		}
		closed := terminalCleanupParent(parent) || !now.Before(input.Deadline)
		if !closed && (parent.Status != ExecutionStatusRunning || parent.LeaseExpiresAt == nil || parent.LeaseOwner == nil || parent.LeaseToken == nil || !parent.LeaseExpiresAt.After(now)) {
			return nil
		}
		if !closed && prepare != nil {
			if err = prepare(tx, child, input); err != nil {
				return err
			}
		}
		// Re-read the wall clock after every possibly blocking Owner lock.
		now, err = DatabaseNow(tx)
		if err != nil {
			return err
		}
		closed = terminalCleanupParent(parent) || !now.Before(input.Deadline)
		if closed {
			basis := "deadline_reached"
			if terminalCleanupParent(parent) {
				basis = "parent_terminal"
			}
			rejection := cleanupRejection{Schema: cleanupRejectionSchema, Code: "cleanup_unstarted_rejected", RequestID: input.RequestID, RequestDigest: input.Digest, ParentExecutionID: input.ParentExecutionID, Module: child.Module, Basis: basis}
			data, _ := json.Marshal(rejection)
			var details models.JSONMap
			if err = json.Unmarshal(data, &details); err != nil {
				return err
			}
			for _, code := range []string{"cleanup_authorization_denied", "cleanup_authorization_unavailable"} {
				if observed, ok := child.ErrorDetails[code].(string); ok {
					if _, e := time.Parse(time.RFC3339Nano, observed); e == nil {
						details[code] = observed
					}
				}
			}
			if err = RejectUnstarted(ctx, tx, identity, map[string]interface{}{"error_details": details}); err != nil {
				return err
			}
			if err = tx.Where("execution_id = ?", child.ExecutionID).First(&child).Error; err != nil {
				return err
			}
			result = &child
			return nil
		}
		if parent.LeaseExpiresAt == nil || !now.Before(*parent.LeaseExpiresAt) {
			return nil
		}
		deadline := input.Deadline
		if parent.LeaseExpiresAt.Before(deadline) {
			deadline = *parent.LeaseExpiresAt
		}
		result, lease, err = ClaimPending(ctx, tx, identity, options, &deadline)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return result, lease, nil
}

type cleanupRejection struct {
	Schema            string `json:"schema"`
	Code              string `json:"code"`
	RequestID         string `json:"request_id"`
	RequestDigest     string `json:"request_digest"`
	ParentExecutionID string `json:"parent_execution_id"`
	Module            string `json:"module"`
	Basis             string `json:"basis"`
}

type CleanupReceipt struct {
	Schema            string                      `json:"schema"`
	RequestID         string                      `json:"request_id"`
	RequestDigest     string                      `json:"request_digest"`
	ExecutionID       string                      `json:"execution_id"`
	ParentExecutionID string                      `json:"parent_execution_id"`
	TenantID          int                         `json:"tenant_id"`
	Module            string                      `json:"module"`
	Attempt           int                         `json:"attempt"`
	Action            string                      `json:"action"`
	CleanupMode       string                      `json:"cleanup_mode,omitempty"`
	Status            string                      `json:"status"`
	Reason            string                      `json:"reason,omitempty"`
	Summary           events.CleanupResultSummary `json:"summary"`
	Impact            events.CleanupImpactSummary `json:"impact"`
	ImpactDigest      string                      `json:"impact_digest"`
}

func (r CleanupReceipt) Validate(e TaskExecution) error {
	input, err := CleanupRequestFromExecution(e)
	if err != nil {
		return err
	}
	if r.Schema != CleanupReceiptSchema || e.TaskType != TaskTypeCleanupExecutor || r.ExecutionID != e.ExecutionID || r.ParentExecutionID != input.ParentExecutionID || r.RequestID != input.RequestID || r.RequestDigest != input.Digest || r.TenantID != e.TenantID || r.Module != e.Module || r.Attempt != e.Attempt || r.Attempt <= 0 || r.Action != input.Action || r.CleanupMode != input.CleanupMode || !events.CleanupDigestValid(r.ImpactDigest) {
		return fmt.Errorf("cleanup receipt binding is invalid")
	}
	switch r.Status {
	case events.CleanupResultSuccess, events.CleanupResultFailed, events.CleanupResultPartialSuccess, events.CleanupResultSkipped, events.CleanupResultTimeout:
	default:
		return fmt.Errorf("invalid cleanup result status")
	}
	switch r.Reason {
	case "", "owner_failed", "owner_unavailable", "authorization_denied", "active_execution", "external_effect_uncertain", "scope_changed", "unsupported_scope":
	default:
		return fmt.Errorf("invalid cleanup safe reason")
	}
	if r.Action == events.CleanupActionScan && (r.Summary.AffectedRecords != 0 || r.Summary.DeletedPhysicalArtifacts != 0 || r.Summary.FreedBytes != 0 || r.Summary.MarkedMissingSource != 0 || r.Summary.MarkedOutdated != 0 || r.Summary.DisabledTaskDefinitions != 0 || r.Summary.DeletedTaskDefinitions != 0) {
		return fmt.Errorf("scan receipt cannot claim actual effects")
	}
	if err = events.ValidateCleanupSummary(r.Summary, r.Impact); err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil || len(data) > events.CleanupProtocolMaxBytes {
		return fmt.Errorf("cleanup receipt exceeds bounds")
	}
	return nil
}

func NewCleanupReceipt(e TaskExecution, result events.CleanupResultData, reason string) (CleanupReceipt, error) {
	input, err := CleanupRequestFromExecution(e)
	if err != nil {
		return CleanupReceipt{}, err
	}
	if result.Module != e.Module || result.TenantID != uint(e.TenantID) || result.TaskID != input.RequestID || result.Action != input.Action || result.CleanupMode != input.CleanupMode || result.Impact == nil {
		return CleanupReceipt{}, fmt.Errorf("owner result binding or impact is missing")
	}
	r := CleanupReceipt{Schema: CleanupReceiptSchema, RequestID: input.RequestID, RequestDigest: input.Digest, ExecutionID: e.ExecutionID, ParentExecutionID: input.ParentExecutionID, TenantID: e.TenantID, Module: e.Module, Attempt: e.Attempt, Action: input.Action, CleanupMode: input.CleanupMode, Status: result.Status, Reason: reason, Summary: result.Summary, Impact: result.Impact.Summary, ImpactDigest: result.Impact.Digest}
	return r, r.Validate(e)
}

// CompleteCleanupWithReceipt must share the Owner private-result transaction.
// It locks only C, never P, and performs the final database-clock lease guard.
func CompleteCleanupWithReceipt(ctx context.Context, db *gorm.DB, lease Lease, receipt CleanupReceipt) error {
	return withLockedAttempt(ctx, db, lease, false, func(tx *gorm.DB, e TaskExecution, now time.Time) error {
		if err := receipt.Validate(e); err != nil {
			return err
		}
		if _, exists := e.Metadata["cleanup_receipt"]; exists {
			return commonapi.ErrConflict
		}
		fields := map[string]interface{}{}
		if err := applyTerminalFields(e, StatusFromCleanupResult(receipt.Status), fields, now); err != nil {
			return err
		}
		metadata := models.JSONMap{}
		for key, value := range e.Metadata {
			metadata[key] = value
		}
		encoded, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		// Persist canonical typed JSON as a string to retain integers and bytes.
		metadata["cleanup_receipt"] = string(encoded)
		fields["metadata"] = metadata
		fields["progress"] = 100
		return requireOwnedRow(ownedActiveExecution(tx, lease, now).Updates(fields), lease.ExecutionID, "complete cleanup with receipt")
	})
}

func ReadCleanupReceipt(e TaskExecution) (*CleanupReceipt, error) {
	raw, exists := e.Metadata["cleanup_receipt"]
	if !exists {
		return nil, nil
	}
	value, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("invalid cleanup receipt representation")
	}
	var receipt CleanupReceipt
	if err := events.CleanupStrictDecode([]byte(value), &receipt); err != nil {
		return nil, err
	}
	if err := receipt.Validate(e); err != nil {
		return nil, err
	}
	if e.Status != StatusFromCleanupResult(receipt.Status) || e.CompletedAt == nil || e.StartedAt == nil {
		return nil, fmt.Errorf("receipt does not have a committed terminal")
	}
	return &receipt, nil
}

// CleanupAdmissionView is a pure response projection, never execution state.
type CleanupAdmissionView struct {
	Schema     string    `json:"schema"`
	State      string    `json:"state"`
	Basis      string    `json:"basis,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
}

// ProjectCleanupAdmission accepts only already-authorized records from one
// read-only snapshot. nil parent means it is not readable; no DB/HTTP is used.
func ProjectCleanupAdmission(child TaskExecution, parent *TaskExecution, observedAt time.Time) *CleanupAdmissionView {
	input, err := CleanupRequestFromExecution(child)
	if err != nil || child.TaskType != TaskTypeCleanupExecutor || observedAt.IsZero() || !cleanupUnstarted(child) {
		return nil
	}
	rejected := false
	if child.Status == ExecutionStatusFailed && child.CompletedAt != nil {
		raw, err := json.Marshal(child.ErrorDetails)
		var proof cleanupRejection
		rejected = err == nil && events.CleanupStrictDecode(raw, &proof) == nil && proof.Schema == cleanupRejectionSchema && proof.Code == "cleanup_unstarted_rejected" && proof.RequestID == input.RequestID && proof.RequestDigest == input.Digest && proof.ParentExecutionID == input.ParentExecutionID && proof.Module == child.Module && (proof.Basis == "parent_terminal" || proof.Basis == "deadline_reached")
	}
	view := &CleanupAdmissionView{Schema: "addp.cleanup-admission-view/v1", ObservedAt: observedAt.UTC()}
	if parent == nil {
		if !rejected {
			return nil
		}
		view.State = "rejected_unstarted"
		return view
	}
	if _, err = cleanupBinding(*parent, child); err != nil {
		return nil
	}
	if terminalCleanupParent(*parent) {
		view.Basis = "parent_terminal"
	} else if rejected && !observedAt.Before(input.Deadline) {
		view.Basis = "deadline_reached"
	} else {
		return nil
	}
	if rejected {
		view.State = "closed_rejected"
	} else if child.Status == ExecutionStatusPending && child.CompletedAt == nil {
		view.State = "closed_unclaimed"
	} else {
		return nil
	}
	return view
}

// NoteCleanupAuthorizationFailure records an Owner's bounded authorization
// diagnostic without claiming or terminating C. It never takes P or touches
// domain state. A concurrent claim/rejection wins and cannot be overwritten.
func NoteCleanupAuthorizationFailure(ctx context.Context, db *gorm.DB, identity PendingIdentity, code string) error {
	if code != "cleanup_authorization_denied" && code != "cleanup_authorization_unavailable" {
		return commonapi.ErrBadRequest
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var child TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id = ? AND tenant_id = ? AND module = ? AND task_type = ? AND source = ?", identity.ExecutionID, identity.TenantID, identity.Module, TaskTypeCleanupExecutor, ModuleSystem).First(&child).Error; err != nil {
			return err
		}
		if child.Status != ExecutionStatusPending || !cleanupUnstarted(child) || child.CompletedAt != nil {
			return nil
		}
		if _, err := CleanupRequestFromExecution(child); err != nil {
			return err
		}
		now, err := DatabaseNow(tx)
		if err != nil {
			return err
		}
		details := child.ErrorDetails
		if details == nil {
			details = models.JSONMap{}
		}
		// The latest fact of each class survives later unstarted rejection.
		details[code] = now.UTC().Format(time.RFC3339Nano)
		return tx.Model(&TaskExecution{}).Where("execution_id = ? AND tenant_id = ? AND status = ?", child.ExecutionID, child.TenantID, ExecutionStatusPending).Updates(map[string]interface{}{"error_details": details, "updated_at": now}).Error
	})
}
