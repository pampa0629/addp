package service

import (
	"context"
	"errors"
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scantask"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
	"time"
)

const scanCancellationKey = "scan_cancellation"

var errScanCancellationRequested = errors.New("Meta scan cancellation requested")

type ScanCancellationActor struct {
	PrincipalID  int64
	MembershipID *int64
}

// Authorization is current and request-scoped. Holding the execution lock
// excludes claim/completion while recording intent; it never cancels an I/O
// context for an already admitted enumeration.
func (s *ScanExecutionService) CancelPGCollection(ctx context.Context, tenantID uint, id string, actor ScanCancellationActor, authorize func(context.Context, uint) error) (*execution.TaskExecution, error) {
	parsed, err := uuid.Parse(id)
	if tenantID == 0 || err != nil || parsed == uuid.Nil || parsed.String() != id || actor.PrincipalID <= 0 || actor.MembershipID == nil || *actor.MembershipID <= 0 || authorize == nil {
		return nil, commonapi.ErrBadRequest
	}
	var run execution.TaskExecution
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tenant_id=? AND execution_id=? AND module=? AND task_type=? AND execution_boundary=?", tenantID, id, execution.ModuleMeta, execution.TaskTypeScan, execution.ExecutionBoundaryBounded).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&run).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commonapi.ErrNotFound
			}
			return err
		}
		config := scanflow.ParseExecutionConfig(run.ExecutionConfig)
		if run.SourceTaskID == nil || config.EngineID == 0 || run.ExecutionConfig["engine_type"] != "postgresql" || config.ScanDepth != "basic" {
			return commonapi.ErrForbidden
		}
		if err := authorize(ctx, config.EngineID); err != nil {
			return err
		}
		if run.IsCompleted() {
			return nil
		}
		now, err := execution.DatabaseNow(tx)
		if err != nil {
			return err
		}
		if _, exists := run.Metadata[scanCancellationKey]; !exists {
			if run.Metadata == nil {
				run.Metadata = commonmodels.JSONMap{}
			}
			run.Metadata[scanCancellationKey] = commonmodels.JSONMap{"requested_at": now.UTC().Format(time.RFC3339Nano), "principal_id": strconv.FormatInt(actor.PrincipalID, 10), "tenant_membership_id": strconv.FormatInt(*actor.MembershipID, 10), "reason": "user_requested"}
			if err := tx.Model(&execution.TaskExecution{}).Where("tenant_id=? AND execution_id=?", tenantID, id).Update("metadata", run.Metadata).Error; err != nil {
				return err
			}
		}
		if run.Status == execution.ExecutionStatusPending {
			if run.Attempt != 0 {
				return commonapi.ErrConflict
			}
			identity := execution.PendingIdentity{ExecutionID: run.ExecutionID, TenantID: run.TenantID, Module: run.Module, TaskType: run.TaskType, Source: run.Source}
			if err := execution.CancelUnstarted(ctx, tx, identity); err != nil {
				return err
			}
			taskID, err := execution.ParseSourceTaskIDUint(run.SourceTaskID)
			if err != nil {
				return err
			}
			if err := tx.Model(&models.ScanTask{}).Where("tenant_id=? AND id=? AND last_execution_id=?", tenantID, taskID, id).Updates(scantask.TaskStatusBackfillFields(id, execution.ExecutionStatusCancelled, now)).Error; err != nil {
				return err
			}
			return tx.Where("tenant_id=? AND execution_id=?", tenantID, id).Take(&run).Error
		}
		if run.Status != execution.ExecutionStatusRunning {
			return commonapi.ErrConflict
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if run.Status == execution.ExecutionStatusCancelled {
		config := scanflow.ParseExecutionConfig(run.ExecutionConfig)
		if s.dedupService != nil {
			key := s.dedupService.GenerateExecutionLockKey(tenantID, config.EngineID, config.ItemID, config.CatalogPaths, config.RefGroups, config.Targets)
			s.releaseExecutionLock(ctx, true, key, id, "release cancelled scan lock failed")
		}
	}
	return &run, nil
}

func (s *ScanExecutionService) checkScanCancellation(ctx context.Context, tenantID int, id string) error {
	var run execution.TaskExecution
	if err := s.db.WithContext(ctx).Select("metadata, status").Where("tenant_id=? AND execution_id=? AND module=? AND task_type=?", tenantID, id, execution.ModuleMeta, execution.TaskTypeScan).Take(&run).Error; err != nil {
		return err
	}
	if _, exists := run.Metadata[scanCancellationKey]; exists {
		return errScanCancellationRequested
	}
	if run.Status == execution.ExecutionStatusCancelled {
		return errScanCancellationRequested
	}
	return nil
}
