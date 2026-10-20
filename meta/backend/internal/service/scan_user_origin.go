package service

import (
	"context"

	commonExecution "github.com/addp/common/execution"
)

// This captures human provenance only. It is not a scan qualification or an
// execution authorization and must not be used for scheduled service work.
func verifiedScanUserOrigin(ctx context.Context, tenantID, userID uint) (*commonExecution.TaskExecution, error) {
	by := int(userID)
	origin := &commonExecution.TaskExecution{TenantID: int(tenantID), TriggeredBy: &by}
	if err := commonExecution.ApplyUserOrigin(ctx, origin); err != nil {
		return nil, err
	}
	return origin, nil
}
