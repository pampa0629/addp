package execution

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// InheritOrchestratorActor verifies an Orchestrator parent execution and copies
// its durable User authorization facts into a child execution. Callers must run
// this inside the same transaction that creates the child execution.
func InheritOrchestratorActor(tx *gorm.DB, execution *TaskExecution) error {
	if tx == nil || execution == nil || execution.TenantID <= 0 {
		return fmt.Errorf("orchestrator child execution context is required")
	}
	parentExecutionID := ""
	if execution.ParentExecutionID != nil {
		parentExecutionID = *execution.ParentExecutionID
	}
	parentExecutionID, err := NormalizeOrchestratorChildContext(execution.Source, parentExecutionID)
	if err != nil {
		return err
	}

	var parent TaskExecution
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("tenant_id = ? AND execution_id = ? AND module = ? AND status = ?",
			execution.TenantID, parentExecutionID, ModuleOrchestrator, ExecutionStatusRunning).
		First(&parent).Error; err != nil {
		return fmt.Errorf("orchestration parent execution is unavailable: %w", err)
	}
	if parent.TaskType != TaskTypeOrchestration {
		return fmt.Errorf("orchestration parent has an invalid task type")
	}
	if err := CopyActorOrigin(execution, &parent); err != nil {
		return err
	}
	execution.ParentExecutionID = &parentExecutionID
	return nil
}

// NormalizeOrchestratorChildContext validates the provenance fields accepted by
// every TaskProvider execution endpoint.
func NormalizeOrchestratorChildContext(source, parentExecutionID string) (string, error) {
	if strings.TrimSpace(source) != ModuleOrchestrator || strings.TrimSpace(parentExecutionID) == "" {
		return "", fmt.Errorf("orchestrator source and parent_execution_id are required")
	}
	parsedParentExecutionID, err := uuid.Parse(strings.TrimSpace(parentExecutionID))
	if err != nil {
		return "", fmt.Errorf("orchestrator parent execution id is invalid: %w", err)
	}
	return parsedParentExecutionID.String(), nil
}
