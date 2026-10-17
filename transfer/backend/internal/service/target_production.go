package service

import (
	"context"
	"fmt"

	plugin "github.com/addp/common/engine/plugin"
	execution "github.com/addp/common/execution"
	"github.com/addp/common/resourcetree"
	"github.com/addp/transfer/internal/executor"
	"github.com/addp/transfer/internal/models"
)

func (s *ExecutionEngineService) beginTableTargetProduction(ctx context.Context, task *models.TransferTask, item *execution.TaskExecution, id uint, target executor.TableTargetPlan, pipeline *executor.TableTransferExecutor) (string, execution.Lease, error) {
	var empty execution.Lease
	if target.Kind != executor.TableEndpointNative || target.ManagedExisting {
		return "", empty, nil
	}
	if _, ok := pipeline.TargetNativePreparer.(plugin.TableTargetPreparer); !ok {
		return "", empty, nil
	}
	if item == nil || task == nil || item.ID != int64(id) || item.TenantID != int(task.TenantID) || s.executionService == nil || s.executionService.taskRepo == nil {
		return "", empty, fmt.Errorf("target production requires the current owner execution")
	}
	lease, ok := s.executionService.leaseForExecution(ctx, id, item.ExecutionID)
	if !ok || lease.TenantID != item.TenantID {
		return "", empty, fmt.Errorf("target production requires the active execution lease")
	}
	businessPath := plugin.EngineCatalogPathWithoutRoot(target.Path)
	parts := make([]string, len(businessPath.Segments))
	for i, segment := range businessPath.Segments {
		parts[i] = segment.Name
	}
	locator := (&resourcetree.ResourceLocator{EngineID: target.Path.EngineID, Path: parts, Type: resourcetree.TypeTable}).ToURI()
	if err := s.executionService.taskRepo.BeginTargetProduction(ctx, lease, locator); err != nil {
		return "", empty, fmt.Errorf("persist target production intent: %w", err)
	}
	return locator, lease, nil
}
