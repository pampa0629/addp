package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func canonicalCleanupScope(scope []string) (string, error) {
	values := make([]string, 0, len(scope))
	seen := map[string]bool{}
	for _, value := range scope {
		if !events.CleanupModuleValid(value) {
			return "", commonapi.ErrBadRequest
		}
		if !seen[value] {
			seen[value] = true
			values = append(values, value)
		}
	}
	sort.Strings(values)
	data, err := json.Marshal(values)
	return string(data), err
}

type cleanupSubmission struct {
	RequestID          string
	TenantID           uint
	RequestedBy        uint
	Action             string
	CleanupMode        string
	Scope              []string
	TriggerType        string
	CauseEvent         string
	SourceOperationID  string
	BasedOnScan        string
	Context            events.CleanupContext
	Confirmation       events.CleanupConfirmation
	EngineConfirmation string
}

// acceptUser freezes caller intent before dispatch. A retry compares the caller
// scope to its original submitted scope and reuses frozen discovery/clock facts;
// registry changes never replace the original child set or renew its deadline.
func (s *cleanupCoordinator) acceptUser(ctx context.Context, p cleanupSubmission, discover func([]string, string) ([]string, error)) (*models.CleanupRequestRecord, error) {
	id, err := uuid.Parse(p.RequestID)
	if err != nil || id == uuid.Nil || id.String() != p.RequestID || p.TenantID == 0 || p.RequestedBy == 0 || p.TriggerType != execution.TriggerTypeManual {
		return nil, commonapi.ErrBadRequest
	}
	requestedScope, err := canonicalCleanupScope(p.Scope)
	if err != nil {
		return nil, err
	}
	parent := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(p.TenantID), Module: execution.ModuleSystem, TaskType: execution.TaskTypeCleanup, Source: execution.ModuleSystem, Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded, TriggerType: p.TriggerType, MaxAttempts: 3}
	if err := execution.ApplyUserOrigin(ctx, &parent); err != nil {
		return nil, err
	}
	if parent.TriggeredBy == nil || *parent.TriggeredBy != int(p.RequestedBy) {
		return nil, commonapi.ErrForbidden
	}
	if err := execution.FreezeDisplayLanguage(&parent, commoni18n.Language(ctx)); err != nil {
		return nil, err
	}
	input := events.CleanupRequest{RequestID: p.RequestID, ParentExecutionID: parent.ExecutionID, TenantID: p.TenantID, RequestedBy: p.RequestedBy, Action: p.Action, CleanupMode: p.CleanupMode, TriggerType: p.TriggerType, CauseEvent: p.CauseEvent, SourceOperationID: p.SourceOperationID, BasedOnScan: p.BasedOnScan, Context: p.Context, Confirmation: p.Confirmation}
	var original models.CleanupRequestRecord
	query := s.db.WithContext(ctx).Where("request_id = ?", p.RequestID)
	if p.SourceOperationID != "" {
		query = query.Or("tenant_id = ? AND source_operation_id = ? AND action = ?", p.TenantID, p.SourceOperationID, p.Action)
	}
	err = query.First(&original).Error
	switch {
	case err == nil:
		if original.TenantID != p.TenantID || original.RequestedBy != p.RequestedBy || original.RequestedScope != requestedScope {
			return nil, commonapi.ErrConflict
		}
		frozen, err := original.Input()
		if err != nil {
			return nil, err
		}
		input.RequestID = original.RequestID
		input.ParentExecutionID = frozen.ParentExecutionID
		input.ExpectedModules = frozen.ExpectedModules
		input.RequestedAt, input.Deadline = frozen.RequestedAt, frozen.Deadline
	case errors.Is(err, gorm.ErrRecordNotFound):
		if discover == nil {
			return nil, commonapi.ErrBadRequest
		}
		input.ExpectedModules, err = discover(p.Scope, p.CauseEvent)
		if err != nil {
			return nil, err
		}
		now, err := execution.DatabaseNow(s.db.WithContext(ctx))
		if err != nil {
			return nil, err
		}
		budget := 30 * time.Second
		if p.Action == events.CleanupActionExecute {
			budget = 5 * time.Minute
		}
		input.RequestedAt, input.Deadline = now, now.Add(budget)
	default:
		return nil, err
	}
	if err := input.Seal(); err != nil {
		return nil, commonapi.ErrBadRequest
	}
	return s.accept(ctx, input, parent, p.EngineConfirmation, p.Scope)
}

// cleanupReceiptSummary only combines already-validated closed receipts.
// Professional output and free text cannot enter the coordinator aggregate.
func cleanupReceiptSummary(receipts []execution.CleanupReceipt) (events.CleanupResultSummary, events.CleanupImpactSummary, string, error) {
	summary := events.CleanupResultSummary{RiskLevel: "low"}
	var impact events.CleanupImpactSummary
	tokens := make([]string, 0, len(receipts))
	for _, r := range receipts {
		addCleanupSummary(&summary, r.Summary)
		impact.Add(r.Impact)
		tokens = append(tokens, r.Module+":"+r.ImpactDigest)
	}
	if len(tokens) == 0 {
		return summary, impact, "", nil
	}
	sort.Strings(tokens)
	combined, err := events.BuildCleanupImpactData([]events.CleanupImpactItem{{StableRef: strings.Join(tokens, "|"), Disposition: events.CleanupImpactWillDelete}}, "")
	if err != nil {
		return summary, impact, "", err
	}
	if err := events.ValidateCleanupSummary(summary, impact); err != nil {
		return summary, impact, "", err
	}
	return summary, impact, combined.Digest, nil
}

func addCleanupSummary(total *events.CleanupResultSummary, value events.CleanupResultSummary) {
	total.ScannedItems += value.ScannedItems
	total.AffectedRecords += value.AffectedRecords
	total.DeletedPhysicalArtifacts += value.DeletedPhysicalArtifacts
	total.FreedBytes += value.FreedBytes
	total.MarkedMissingSource += value.MarkedMissingSource
	total.MarkedOutdated += value.MarkedOutdated
	total.DisabledTaskDefinitions += value.DisabledTaskDefinitions
	total.DeletedTaskDefinitions += value.DeletedTaskDefinitions
	total.SkippedItems += value.SkippedItems
	total.ErrorCount += value.ErrorCount
	total.RiskLevel = higherRisk(total.RiskLevel, value.RiskLevel)
}
