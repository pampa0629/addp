package execution

import (
	"context"
	"fmt"
	"slices"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/events"
	"gorm.io/gorm"
)

// ReadCleanupScanReceipt reads only this Owner's immutable public scan evidence.
// It grants no authority, reads no private workflow state and takes no P/C locks.
func ReadCleanupScanReceipt(ctx context.Context, db *gorm.DB, current TaskExecution) (*CleanupReceipt, error) {
	input, err := CleanupRequestFromExecution(current)
	if err != nil || current.TaskType != TaskTypeCleanupExecutor || input.Action != events.CleanupActionExecute {
		return nil, fmt.Errorf("%w: cleanup execute baseline is invalid", commonapi.ErrConflict)
	}
	requestID := "CAST(execution_config ->> 'cleanup_request' AS jsonb) ->> 'request_id'"
	if db.Dialector.Name() == "sqlite" {
		requestID = "json_extract(json_extract(execution_config, '$.cleanup_request'), '$.request_id')"
	}
	var children []TaskExecution
	if err := db.WithContext(ctx).Where("tenant_id = ? AND module = ? AND task_type = ? AND source = ?", current.TenantID, current.Module, TaskTypeCleanupExecutor, ModuleSystem).
		Where(requestID+" = ?", input.BasedOnScan).Limit(2).Find(&children).Error; err != nil {
		return nil, err
	}
	if len(children) != 1 || children[0].ParentExecutionID == nil {
		return nil, fmt.Errorf("%w: cleanup scan baseline is missing or ambiguous", commonapi.ErrConflict)
	}
	child := children[0]
	var parent TaskExecution
	if err := db.WithContext(ctx).Where("execution_id = ? AND tenant_id = ?", *child.ParentExecutionID, current.TenantID).First(&parent).Error; err != nil {
		return nil, err
	}
	scan, err := cleanupBinding(parent, child)
	if err != nil || scan.RequestID != input.BasedOnScan || scan.Action != events.CleanupActionScan ||
		scan.CauseEvent != input.CauseEvent || scan.Context.EngineID != input.Context.EngineID || scan.Context.TenantID != input.Context.TenantID || !slices.Equal(scan.ExpectedModules, input.ExpectedModules) ||
		parent.Status != ExecutionStatusSuccess || parent.CompletedAt == nil || child.Status != ExecutionStatusSuccess {
		return nil, fmt.Errorf("%w: cleanup scan baseline scope or terminal is invalid", commonapi.ErrConflict)
	}
	receipt, err := ReadCleanupReceipt(child)
	if err != nil || receipt == nil || receipt.Status != events.CleanupResultSuccess {
		return nil, fmt.Errorf("%w: cleanup scan baseline receipt is invalid", commonapi.ErrConflict)
	}
	return receipt, nil
}
