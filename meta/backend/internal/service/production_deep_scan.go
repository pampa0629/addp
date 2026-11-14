package service

import (
	"context"
	"errors"
	"reflect"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/common/production"
	"github.com/addp/meta/internal/scantask"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errProductionBasicIncomplete = errors.New("production basic identification did not succeed")

// Persist the dependent execution before returning. No process-local callback
// or producer lifetime is needed to carry the enhancement across a restart.
func (s *ScanExecutionService) EnsureProductionDeepScan(ctx context.Context, tenant uint, ref production.Reference) (*client.MetaProductionDeepScan, error) {
	basic, err := s.EnsureProductionBasicScan(ctx, tenant, ref)
	if err != nil {
		return nil, err
	}
	var result *client.MetaProductionDeepScan
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		original, err := productionScanRecord(tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND execution_id=? AND module=? AND task_type=?", tenant, basic.ScanExecutionID, execution.ModuleMeta, execution.TaskTypeScan))
		if err != nil {
			return err
		}
		receipt, err := frozenProductionReceipt(original)
		if err != nil || receipt.Reference != ref || original.ExecutionConfig["scan_depth"] != "basic" {
			return commonapi.ErrConflict
		}
		child, err := productionScanRecord(tx.Where("tenant_id=? AND module=? AND task_type=? AND execution_config->>'production_basic_execution_id'=?", tenant, execution.ModuleMeta, execution.TaskTypeScan, original.ExecutionID).Order("id"))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			req, err := productionScanRequest(*receipt)
			if err != nil {
				return err
			}
			var user uint
			if receipt.Origin != nil {
				user = uint(receipt.Origin.PrincipalID)
			}
			child = scantask.NewManualExecution(tenant, user, req.EngineID, 0, scantask.NormalizeStorageType(original.ExecutionConfig["engine_type"].(string)), req.Targets, nil, req.RefGroups, "deep", true, ref.Owner, time.Now())
			child.ParentExecutionID = &ref.ExecutionID
			child.ActorPrincipalID, child.ActorTenantMembershipID, child.IssuedAuthorizationVersion = original.ActorPrincipalID, original.ActorTenantMembershipID, original.IssuedAuthorizationVersion
			if receipt.Origin == nil {
				child.TriggeredBy = nil
			}
			child.ExecutionConfig["engine_version"] = original.ExecutionConfig["engine_version"]
			child.ExecutionConfig["engine_type"] = original.ExecutionConfig["engine_type"]
			child.ExecutionConfig["initial_production_receipt"] = receipt
			child.ExecutionConfig["production_basic_execution_id"] = original.ExecutionID
			if err := tx.Create(child).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		frozen, err := frozenProductionReceipt(child)
		if err != nil || !reflect.DeepEqual(receipt, frozen) || child.ExecutionConfig["scan_depth"] != "deep" {
			return commonapi.ErrConflict
		}
		result = &client.MetaProductionDeepScan{Reference: ref, TenantID: int64(tenant), BasicScanExecutionID: original.ExecutionID, ScanExecutionID: child.ExecutionID, Status: child.Status, EngineVersion: receipt.TargetEngineVersion}
		return result.Validate()
	})
	return result, err
}

// A dependency is provenance, never permission to widen the frozen target.
func (s *ScanExecutionService) checkProductionBasicDependency(ctx context.Context, db *gorm.DB, run *execution.TaskExecution, receipt *production.Receipt) error {
	if run.ExecutionConfig["scan_depth"] != "deep" {
		return nil
	}
	basic, err := productionScanRecord(db.WithContext(ctx).Where("tenant_id=? AND execution_id=? AND module=? AND task_type=?", run.TenantID, run.ExecutionConfig["production_basic_execution_id"], execution.ModuleMeta, execution.TaskTypeScan))
	if err != nil {
		return err
	}
	original, err := frozenProductionReceipt(basic)
	if err != nil || basic.ExecutionConfig["scan_depth"] != "basic" || !reflect.DeepEqual(original, receipt) {
		return commonapi.ErrForbidden
	}
	if basic.Status != execution.ExecutionStatusSuccess {
		return errProductionBasicIncomplete
	}
	// A successful basic run must still identify the exact current DataItem.
	_, err = s.productionIdentification(ctx, db, basic, uint(run.TenantID), receipt.Reference)
	return err
}
