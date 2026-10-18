package service

import (
	"context"
	"fmt"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
)

func managerExecutionProgressCallback(ctx context.Context, endpoint string, tenantID uint, executionID string) (commonModels.JSONMap, error) {
	lease, err := requireManagerExecutionLease(ctx, tenantID, executionID)
	if err != nil {
		return nil, err
	}
	return commonModels.JSONMap{
		"endpoint":     endpoint,
		"tenant_id":    tenantID,
		"execution_id": strings.TrimSpace(executionID),
		"attempt":      lease.Attempt,
		"lease_token":  lease.Token,
	}, nil
}

func requireManagerExecutionLease(ctx context.Context, tenantID uint, executionID string) (commonExecution.Lease, error) {
	executionID = strings.TrimSpace(executionID)
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || lease.ExecutionID != executionID || lease.TenantID != int(tenantID) || lease.Attempt <= 0 || strings.TrimSpace(lease.Token) == "" {
		return commonExecution.Lease{}, fmt.Errorf("%w: Manager execution %s requires its claimed lease", commonAPI.ErrConflict, executionID)
	}
	return lease, nil
}

// Runtime messages and metadata stay in the Owner record; events contain only the percentage.
func updateManagerExecutionProgress(ctx context.Context, repo *commonExecution.TaskExecutionRepository, tenantID uint, executionID string, progress int, fields map[string]interface{}) error {
	lease, err := requireManagerExecutionLease(ctx, tenantID, executionID)
	if err != nil {
		return err
	}
	if repo == nil || progress < 0 || progress > 100 || fields["progress"] != progress {
		return fmt.Errorf("invalid Manager execution progress update")
	}
	return repo.UpdateWithEvent(ctx, lease, fields, commonExecution.EventInput{Kind: "progress", Counters: map[string]int64{"progress": int64(progress)}})
}
