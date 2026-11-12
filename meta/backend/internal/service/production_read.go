package service

import (
	"context"
	"reflect"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/scanresource"
)

// Requalify the frozen owner facts before entering source IO. Subsequent reads
// recheck the current lease and exact plan without granting a user data access.
func (s *ScanExecutionService) productionReadContext(ctx context.Context, run *execution.TaskExecution, lease execution.Lease) (context.Context, error) {
	precise, err := productionScanRecord(s.db.WithContext(ctx).Where("tenant_id=? AND execution_id=? AND module=? AND task_type=?", run.TenantID, run.ExecutionID, execution.ModuleMeta, execution.TaskTypeScan))
	if err != nil {
		return nil, err
	}
	run = precise
	receipt, err := frozenProductionReceipt(run)
	if err != nil {
		return nil, err
	}
	if s.productionRegistry == nil {
		return nil, ErrScanPlanFactsUnavailable
	}
	current, err := s.productionRegistry.Read(ctx, uint(run.TenantID), receipt.Reference)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(current, receipt) {
		return nil, commonapi.ErrForbidden
	}
	digest, err := execution.ComputeMetaScanExecutionPlanDigest(mustScanJSON(run.ExecutionConfig))
	if err != nil {
		return nil, err
	}
	check := scanresource.CollectionReadCheck(func(call context.Context, engine *commonmodels.Engine) error {
		if err := s.checkScanCancellation(call, run.TenantID, run.ExecutionID); err != nil {
			return err
		}
		if engine != nil && (engine.ID != receipt.Primary.EngineID || engine.TenantID == nil || int64(*engine.TenantID) != receipt.TenantID || engine.Version != receipt.TargetEngineVersion || engine.LifecycleState != "active") {
			return commonapi.ErrForbidden
		}
		active, err := productionScanRecord(s.db.WithContext(call).Where("tenant_id=? AND execution_id=? AND module=? AND task_type=? AND status='running' AND attempt=? AND lease_token=? AND lease_owner=? AND lease_expires_at>clock_timestamp()", run.TenantID, run.ExecutionID, execution.ModuleMeta, execution.TaskTypeScan, lease.Attempt, lease.Token, lease.Owner))
		if err != nil {
			return err
		}
		frozen, err := frozenProductionReceipt(active)
		if err != nil || !reflect.DeepEqual(frozen, receipt) {
			return commonapi.ErrForbidden
		}
		nowDigest, err := execution.ComputeMetaScanExecutionPlanDigest(mustScanJSON(active.ExecutionConfig))
		if err != nil || nowDigest != digest {
			return commonapi.ErrForbidden
		}
		return nil
	})
	if err := check(ctx, nil); err != nil {
		return nil, err
	}
	targets := make([]plugin.EngineCatalogPath, len(receipt.Objects))
	for i, object := range receipt.Objects {
		targets[i] = object.Target
	}
	return scanresource.WithCommittedContentTargets(scanresource.WithCollectionReadCheck(ctx, check), targets), nil
}
