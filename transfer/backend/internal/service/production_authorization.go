package service

import (
	"context"
	"errors"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Written only as part of the successful physical execution commit. Existing
// successful executions without this marker are never backfilled or granted.
func initialProductionAuthorization(item *execution.TaskExecution, now time.Time) *models.ProductionAuthorization {
	if item == nil {
		return nil
	}
	if item.Module != execution.ModuleTransfer || item.TaskType != execution.TaskTypeSync || item.ExecutionBoundary != execution.ExecutionBoundaryBounded || item.Attempt != 1 || item.RetryOfExecutionID != nil {
		return nil
	}
	copy := execution.TaskExecution{TenantID: item.TenantID}
	if execution.CopyActorOrigin(&copy, item) != nil {
		return nil
	}
	receipt, err := models.DecodeTargetProduction(item.Metadata[models.TargetProductionMetadataKey])
	if err != nil || receipt.TenantID != item.TenantID || receipt.ExecutionID != item.ExecutionID || receipt.Attempt != item.Attempt || receipt.RetryOfExecutionID != nil || receipt.Phase != models.TargetProductionCommitted || !receipt.CreatedAndCommitted || receipt.Preparation != plugin.TableTargetCreated {
		return nil
	}
	return &models.ProductionAuthorization{SchemaVersion: "transfer.initial-authorization/v1", Status: "pending", NextAttemptUnix: now.Unix()}
}

type initialGrantRuntime interface {
	resolve(context.Context, uint, string) (*authorization.InitialProductionLookup, error)
	issue(context.Context, uint, string) (*authorization.InitialProductionResult, error)
}
type systemInitialGrantRuntime struct{ client *client.SystemServiceClient }

func (c systemInitialGrantRuntime) resolve(ctx context.Context, tenant uint, id string) (*authorization.InitialProductionLookup, error) {
	return c.client.WithTenantID(tenant).ResolveInitialTransferGrant(ctx, id)
}
func (c systemInitialGrantRuntime) issue(ctx context.Context, tenant uint, id string) (*authorization.InitialProductionResult, error) {
	return c.client.WithTenantID(tenant).IssueInitialTransferGrant(ctx, id)
}

type productionBasicScanner interface {
	EnsureTransferProductionBasicScan(context.Context, uint, string) (*client.MetaProductionBasicScan, error)
}

type ProductionAuthorizationContinuation struct {
	executions *ExecutionService
	runtime    initialGrantRuntime
	meta       productionBasicScanner
}

func NewProductionAuthorizationContinuation(executions *ExecutionService, system *client.SystemServiceClient, meta *client.MetaClient) *ProductionAuthorizationContinuation {
	return &ProductionAuthorizationContinuation{executions: executions, runtime: systemInitialGrantRuntime{system}, meta: meta}
}

// Runtime registration gates delivery, not physical success or module Ready.
// The caller joins this loop during shutdown. It never holds a DB lock over IO.
func (c *ProductionAuthorizationContinuation) Run(ctx context.Context, ready func() bool, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ready == nil || !ready() {
				continue
			}
			step, cancel := context.WithTimeout(ctx, 30*time.Second)
			if err := c.runOne(step, time.Now().UTC()); err != nil {
				logger.L().Warn("任务产出初始授权续办暂未完成", "error_code", "initial_authorization_continuation_unavailable")
			}
			cancel()
		}
	}
}

func (c *ProductionAuthorizationContinuation) runOne(ctx context.Context, now time.Time) error {
	if c == nil || c.executions == nil || c.executions.db == nil || c.runtime == nil || c.meta == nil {
		return errors.New("initial authorization continuation unavailable")
	}
	var item execution.TaskExecution
	var state models.ProductionAuthorization
	err := c.executions.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Model(&execution.TaskExecution{}).Where("module=? AND task_type=? AND execution_boundary=? AND status=?", execution.ModuleTransfer, execution.TaskTypeSync, execution.ExecutionBoundaryBounded, execution.ExecutionStatusSuccess).
			Where("metadata->'initial_authorization'->>'status'='pending' AND CAST(metadata->'initial_authorization'->>'next_attempt_unix' AS BIGINT)<=?", now.Unix()).
			Order("CAST(metadata->'initial_authorization'->>'next_attempt_unix' AS BIGINT),id").Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Take(&item).Error
		if err != nil {
			return err
		}
		state, err = models.DecodeProductionAuthorization(item.Metadata[models.ProductionAuthorizationMetadataKey])
		if err != nil {
			return err
		}
		if initialProductionAuthorization(&item, now) == nil {
			return errors.New("initial authorization owner evidence unavailable")
		}
		state.Attempts++
		delay := 30 * time.Second
		for i := 1; i < state.Attempts && delay < 5*time.Minute; i++ {
			delay *= 2
		}
		if delay > 5*time.Minute {
			delay = 5 * time.Minute
		}
		state.NextAttemptUnix = now.Add(delay).Unix()
		item.Metadata[models.ProductionAuthorizationMetadataKey] = state
		return tx.Model(&item).Update("metadata", item.Metadata).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	// An unknown previous response must be resolved before another issuance.
	lookup, err := c.runtime.resolve(ctx, uint(item.TenantID), item.ExecutionID)
	code := "system_unavailable"
	var result *authorization.InitialProductionResult
	if err == nil && lookup != nil && lookup.Validate() == nil {
		if lookup.Found {
			result = lookup.Result
		} else {
			result, err = c.runtime.issue(ctx, uint(item.TenantID), item.ExecutionID)
			code = "authorization_pending"
			if err != nil {
				// Only System's pending response asks for technical preparation;
				// transport failures/403 never authorize scanning arbitrary targets.
				pendingCode, _ := client.TenantAPIErrorCode(err)
				if status, ok := client.TenantAPIStatusCode(err); ok && status == 503 && pendingCode == "initial_production_pending" {
					scan, scanErr := c.meta.EnsureTransferProductionBasicScan(ctx, uint(item.TenantID), item.ExecutionID)
					if scanErr == nil && scan != nil && scan.Validate() == nil && scan.ProductionExecutionID == item.ExecutionID {
						code = "scan_pending"
						if scan.Status == execution.ExecutionStatusFailed || scan.Status == execution.ExecutionStatusTimeout || scan.Status == execution.ExecutionStatusCancelled {
							code = "scan_failed"
						}
					}
				}
			}
		}
	}
	if err == nil && result != nil && result.Validate() == nil && result.TenantID == int64(item.TenantID) && result.ExecutionID.String() == item.ExecutionID {
		state.Status = result.Outcome
		state.Code = result.DenialCode
		if result.Grant != nil {
			state.GrantRequestID = result.Grant.RequestID.String()
		}
	} else {
		state.Code = code
	}
	return c.executions.withLockedExecution(ctx, uint(item.ID), func(tx *gorm.DB, current *execution.TaskExecution) error {
		prior, err := models.DecodeProductionAuthorization(current.Metadata[models.ProductionAuthorizationMetadataKey])
		if err != nil {
			return err
		}
		// A delayed duplicate cannot overwrite a newer claim or a terminal result.
		if current.TenantID != item.TenantID || current.ExecutionID != item.ExecutionID || current.Status != execution.ExecutionStatusSuccess || prior.Status != "pending" || prior.Attempts != state.Attempts {
			return nil
		}
		current.Metadata[models.ProductionAuthorizationMetadataKey] = state
		return tx.Model(current).Update("metadata", current.Metadata).Error
	})
}

func setInitialProductionAuthorization(item *execution.TaskExecution, updates map[string]interface{}, now time.Time) {
	state := initialProductionAuthorization(item, now)
	if state == nil {
		return
	}
	metadata := item.Metadata
	if metadata == nil {
		metadata = commonmodels.JSONMap{}
	}
	if _, exists := metadata[models.ProductionAuthorizationMetadataKey]; exists {
		return
	}
	metadata[models.ProductionAuthorizationMetadataKey] = state
	updates["metadata"] = metadata
}
