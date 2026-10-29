package service

import (
	"context"
	"encoding/json"
	"fmt"
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// engineCleanupState is a private System workflow read, never a public DTO or
// a substitute for a current user's Owner read decisions.
type engineCleanupState struct {
	Task         models.CleanupTask
	Cutoff       *models.CleanupCutoff
	Confirmation models.CleanupEngineConfirmation
	Parent       execution.TaskExecution
}

func (s *CleanupOrchestratorService) engineStatus(ctx context.Context, id string, tenant, actor, engine uint) (*engineCleanupState, error) {
	snap, err := s.ownedSnapshot(ctx, id, tenant, actor)
	if err != nil {
		return nil, err
	}
	if snap.Input.CauseEvent != events.CleanupCauseEngineDeleting || snap.Input.Context.EngineID != engine {
		return nil, ErrDeletionAssessmentInvalid
	}
	var confirmation models.CleanupEngineConfirmation
	if events.CleanupStrictDecode([]byte(snap.Record.EngineConfirmation), &confirmation) != nil {
		return nil, ErrDeletionAssessmentInvalid
	}
	cutoff, err := snap.Record.FrozenCutoff()
	if err != nil {
		return nil, err
	}
	return &engineCleanupState{Task: cleanupPublicTask(snap), Parent: snap.Parent, Cutoff: cutoff, Confirmation: confirmation}, nil
}
func (s *CleanupOrchestratorService) CreateEngineDeletionAssessment(ctx context.Context, requestID string, tenant, actor, engine uint, policy string) (string, error) {
	private, _ := json.Marshal(models.CleanupEngineConfirmation{Phase: "preflight", ExternalArtifactPolicy: policy})
	record, err := s.coordinator.acceptUser(ctx, cleanupSubmission{RequestID: requestID, TenantID: tenant, RequestedBy: actor, Action: events.CleanupActionScan, TriggerType: execution.TriggerTypeManual, CauseEvent: events.CleanupCauseEngineDeleting, Context: events.CleanupContext{EngineID: engine}, EngineConfirmation: string(private)}, s.cleanupDiscovery(ctx))
	if err != nil {
		return "", err
	}
	return record.RequestID, nil
}

// BeginEngineCleanup commits the engine transition and the authoritative scan
// acceptance together. A failed permission check or audit rolls back both.
func (s *CleanupOrchestratorService) BeginEngineCleanup(ctx context.Context, engine *models.Engine, assessment, digest string) error {
	if engine == nil || engine.TenantID == nil || engine.DeletionRequestedBy == nil {
		return commonapi.ErrBadRequest
	}
	var committed models.Engine
	err := s.coordinator.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current := *engine
		if err := repository.NewEngineRepository(tx).Update(&current); err != nil {
			return err
		}
		coordinator := *s.coordinator
		coordinator.db = tx
		private, _ := json.Marshal(models.CleanupEngineConfirmation{Phase: "validation", ExternalArtifactPolicy: engine.ExternalArtifactPolicy, AssessmentID: assessment, ImpactDigest: digest})
		record, err := coordinator.acceptUser(ctx, cleanupSubmission{RequestID: uuid.NewString(), TenantID: *engine.TenantID, RequestedBy: *engine.DeletionRequestedBy, Action: events.CleanupActionScan, TriggerType: execution.TriggerTypeManual, CauseEvent: events.CleanupCauseEngineDeleting, SourceOperationID: fmt.Sprintf("engine-delete:%d:%s", engine.ID, assessment), Context: events.CleanupContext{EngineID: engine.ID}, EngineConfirmation: string(private)}, s.cleanupDiscovery(ctx))
		if err != nil {
			return err
		}
		current.DeletionScanTaskID = &record.RequestID
		if err := repository.NewEngineRepository(tx).Update(&current); err != nil {
			return err
		}
		committed = current
		return nil
	})
	if err == nil {
		*engine = committed
	}
	return err
}

// createEngineExecute is invoked only for the persisted, confirmed workflow.
// Origin is copied from its immutable scan P; issuing current authority still
// checks active tenant/membership/version/permission. No Bearer is recovered.
func (s *CleanupOrchestratorService) createEngineExecute(ctx context.Context, engineID uint, scanID string) (string, error) {
	var result string
	err := s.coordinator.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var engine models.Engine
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&engine, engineID).Error; err != nil {
			return err
		}
		if engine.LifecycleState != models.EngineLifecycleDeleting || engine.TenantID == nil || engine.DeletionRequestedBy == nil || engine.DeletionScanTaskID == nil || *engine.DeletionScanTaskID != scanID || engine.DeletionError != "" {
			return commonapi.ErrConflict
		}
		if engine.DeletionExecuteTaskID != nil {
			result = *engine.DeletionExecuteTaskID
			return nil
		}
		scoped := *s
		coordinator := *s.coordinator
		coordinator.db = tx
		scoped.coordinator = &coordinator
		state, err := scoped.engineStatus(ctx, scanID, *engine.TenantID, *engine.DeletionRequestedBy, engineID)
		if err != nil {
			return err
		}
		impact, digest, err := validatedDeletionImpact(state)
		if err != nil || state.Confirmation.Phase != "validation" || state.Confirmation.ImpactDigest != digest || impact.Running != 0 || state.Confirmation.ExternalArtifactPolicy != engine.ExternalArtifactPolicy {
			return ErrDeletionImpactChanged
		}
		now, err := execution.DatabaseNow(tx)
		if err != nil {
			return err
		}
		summary, _, _, err := cleanupReceiptSummary(state.Cutoff.Receipts)
		if err != nil {
			return err
		}
		input := events.CleanupRequest{RequestID: uuid.NewSHA1(uuid.NameSpaceURL, []byte("addp:cleanup:engine-execute:"+scanID)).String(), ParentExecutionID: uuid.NewString(), TenantID: *engine.TenantID, RequestedBy: *engine.DeletionRequestedBy, Action: events.CleanupActionExecute, CleanupMode: events.CleanupModePhysical, TriggerType: execution.TriggerTypeManual, CauseEvent: events.CleanupCauseEngineDeleting, SourceOperationID: "engine-delete-execute:" + scanID, ExpectedModules: state.Task.ExpectedModules, BasedOnScan: scanID, Context: events.CleanupContext{EngineID: engineID, ExternalArtifactPolicy: engine.ExternalArtifactPolicy}, Confirmation: events.CleanupConfirmation{Confirmed: true, TextProvided: true, RiskLevel: summary.RiskLevel, ImpactDigest: digest}, RequestedAt: now, Deadline: now.Add(5 * time.Minute)}
		if err := input.Seal(); err != nil {
			return err
		}
		parent := execution.TaskExecution{ExecutionID: input.ParentExecutionID, TenantID: int(input.TenantID), Module: execution.ModuleSystem, TaskType: execution.TaskTypeCleanup, Source: execution.ModuleSystem, Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded, TriggerType: input.TriggerType, MaxAttempts: 3}
		if err := execution.CopyActorOrigin(&parent, &state.Parent); err != nil {
			return err
		}
		lang, err := execution.DisplayLanguage(&state.Parent)
		if err != nil {
			return err
		}
		if err := execution.FreezeDisplayLanguage(&parent, lang); err != nil {
			return err
		}
		private, _ := json.Marshal(state.Confirmation)
		record, err := coordinator.accept(ctx, input, parent, string(private), state.Task.ExpectedModules)
		if err != nil {
			return err
		}
		result = record.RequestID
		engine.DeletionExecuteTaskID = &result
		return repository.NewEngineRepository(tx).Update(&engine)
	})
	return result, err
}
