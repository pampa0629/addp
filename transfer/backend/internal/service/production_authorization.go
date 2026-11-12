package service

import (
	"context"
	"errors"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/common/logger"
	"github.com/addp/common/production"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type initialGrantRuntime interface {
	resolve(context.Context, uint, string) (*authorization.InitialProductionLookup, error)
	issue(context.Context, uint, string) (*authorization.InitialProductionResult, error)
}
type systemInitialGrantRuntime struct{ client *client.SystemServiceClient }

func (c systemInitialGrantRuntime) resolve(ctx context.Context, tenant uint, id string) (*authorization.InitialProductionLookup, error) {
	return c.client.WithTenantID(tenant).ResolveInitialProductionGrant(ctx, production.Reference{Owner: "transfer", ExecutionID: id, ResultID: id})
}
func (c systemInitialGrantRuntime) issue(ctx context.Context, tenant uint, id string) (*authorization.InitialProductionResult, error) {
	return c.client.WithTenantID(tenant).IssueInitialProductionGrant(ctx, production.Reference{Owner: "transfer", ExecutionID: id, ResultID: id})
}

type productionBasicScanner interface {
	EnsureProductionBasicScan(context.Context, uint, production.Reference) (*client.MetaProductionIdentification, error)
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
	if c == nil || c.executions == nil || c.executions.db == nil || c.runtime == nil {
		return errors.New("initial authorization continuation unavailable")
	}
	var item execution.TaskExecution
	var state models.ProductionAuthorization
	err := c.executions.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Model(&execution.TaskExecution{}).Where("module=? AND task_type=? AND execution_boundary=?", execution.ModuleTransfer, execution.TaskTypeSync, execution.ExecutionBoundaryBounded).
			Where("metadata->'initial_authorization'->>'status'='pending' AND CAST(metadata->'initial_authorization'->>'next_attempt_unix' AS BIGINT)<=?", now.Unix()).
			Order("CAST(metadata->'initial_authorization'->>'next_attempt_unix' AS BIGINT),id").Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Take(&item).Error
		if err != nil {
			return err
		}
		state, err = models.DecodeProductionAuthorization(item.Metadata[models.ProductionAuthorizationMetadataKey])
		if err != nil {
			return err
		}
		state.Claim(now)
		item.Metadata[models.ProductionAuthorizationMetadataKey] = state
		return tx.Model(&item).Update("metadata", item.Metadata).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	ref := production.Reference{Owner: "transfer", ExecutionID: item.ExecutionID, ResultID: item.ExecutionID}
	result, code, err := client.ContinueInitialProduction(ctx, uint(item.TenantID), ref, transferGrantDelivery{c.runtime, uint(item.TenantID)}, c.meta)
	if err == nil && result != nil {
		state.Status, state.Code = result.Outcome, result.DenialCode
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
		if current.TenantID != item.TenantID || current.ExecutionID != item.ExecutionID || prior.Status != "pending" || prior.Attempts != state.Attempts {
			return nil
		}
		current.Metadata[models.ProductionAuthorizationMetadataKey] = state
		return tx.Model(current).Update("metadata", current.Metadata).Error
	})
}

// Adapt the native Transfer execution identity to the shared result delivery.
type transferGrantDelivery struct {
	runtime initialGrantRuntime
	tenant  uint
}

func (d transferGrantDelivery) ResolveInitialProductionGrant(ctx context.Context, ref production.Reference) (*authorization.InitialProductionLookup, error) {
	return d.runtime.resolve(ctx, d.tenant, ref.ExecutionID)
}
func (d transferGrantDelivery) IssueInitialProductionGrant(ctx context.Context, ref production.Reference) (*authorization.InitialProductionResult, error) {
	return d.runtime.issue(ctx, d.tenant, ref.ExecutionID)
}
