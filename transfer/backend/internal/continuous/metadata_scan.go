package continuous

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	commonClient "github.com/addp/common/client"
	engineplugin "github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
)

type PreparedTargetMetadataScanner interface {
	ScanPreparedTarget(ctx context.Context, claim repository.RuntimeLeaseClaim, plan *planner.ContinuousPlan) error
}

type InitialMetadataScanStore interface {
	ClaimInitialMetadataScan(ctx context.Context, claim repository.RuntimeLeaseClaim, now time.Time, claimTTL time.Duration) (*models.TransferTask, bool, error)
	CompleteInitialMetadataScan(
		ctx context.Context,
		claim repository.RuntimeLeaseClaim,
		claimToken string,
		status models.InitialMetadataScanStatus,
		metaExecutionID, errorMessage string,
		now time.Time,
	) (*models.TransferTask, bool, error)
}

// TargetMetadataScanner 在 continuous target 结构建立后提交一次 Meta deep scan。
// Meta API 失败会被持久化但不阻断数据面；仓储或 fencing 错误仍返回给 runtime。
type TargetMetadataScanner struct {
	Store    InitialMetadataScanStore
	Client   *commonClient.MetaClient
	ClaimTTL time.Duration
	Now      func() time.Time
	Logger   *slog.Logger
}

func (s *TargetMetadataScanner) ScanPreparedTarget(ctx context.Context, claim repository.RuntimeLeaseClaim, plan *planner.ContinuousPlan) error {
	if !claim.Task.AutoScanMetadata {
		return nil
	}
	if s == nil || s.Store == nil || s.Client == nil {
		return fmt.Errorf("continuous target metadata scanner dependencies are required")
	}
	engineID, catalogPaths, err := preparedTargetMetadataScope(plan)
	if err != nil {
		return err
	}
	now := s.Now
	if now == nil {
		now = time.Now
	}
	claimTTL := s.ClaimTTL
	if claimTTL <= 0 {
		claimTTL = 2 * time.Minute
	}
	task, owned, err := s.Store.ClaimInitialMetadataScan(ctx, claim, now(), claimTTL)
	if err != nil || !owned {
		return err
	}
	claimToken := task.InitialMetadataScanClaimToken
	run, scanErr := s.Client.WithTenantID(task.TenantID).CreateManualScanRun(commonClient.MetaScanOptions{
		EngineID: engineID, CatalogPaths: catalogPaths,
		ScanDepth: "deep", Force: true,
		TriggerType: commonExecution.TriggerTypeManual,
		Source:      commonExecution.ModuleTransfer,
	})
	if scanErr != nil {
		_, completed, completeErr := s.Store.CompleteInitialMetadataScan(
			ctx, claim, claimToken, models.InitialMetadataScanFailed, "", scanErr.Error(), now(),
		)
		if completeErr != nil {
			return fmt.Errorf("record failed continuous target metadata scan: %w", completeErr)
		}
		if s.Logger != nil {
			s.Logger.Error("failed to submit continuous target metadata scan",
				"error", scanErr, "task_id", task.ID, "engine_id", engineID, "catalog_paths", catalogPaths, "owned", completed)
		}
		return nil
	}
	_, completed, err := s.Store.CompleteInitialMetadataScan(
		ctx, claim, claimToken, models.InitialMetadataScanSuccess, run.ExecutionID, "", now(),
	)
	if err != nil {
		return fmt.Errorf("record successful continuous target metadata scan: %w", err)
	}
	if s.Logger != nil {
		s.Logger.Info("continuous target metadata scan submitted",
			"task_id", task.ID, "engine_id", engineID, "catalog_paths", catalogPaths,
			"meta_execution_id", run.ExecutionID, "owned", completed)
	}
	return nil
}

func preparedTargetMetadataScope(plan *planner.ContinuousPlan) (uint, []string, error) {
	if plan == nil {
		return 0, nil, fmt.Errorf("continuous target metadata scan requires a plan")
	}
	engineID := plan.Target.EngineID
	if engineID == 0 {
		engineID = plan.Target.Path.EngineID
	}
	if engineID == 0 {
		return 0, nil, fmt.Errorf("continuous target metadata scan requires target engine ID")
	}
	if len(plan.Target.Path.Segments) < 2 {
		return 0, nil, fmt.Errorf("continuous target metadata scan requires a target parent catalog path")
	}
	parent := engineplugin.CatalogPath{
		Version:  plan.Target.Path.Version,
		EngineID: engineID,
		Segments: append([]engineplugin.CatalogSegment(nil), plan.Target.Path.Segments[:len(plan.Target.Path.Segments)-1]...),
	}
	path := parent.StringPath()
	if path == "" {
		return 0, nil, fmt.Errorf("continuous target metadata scan parent catalog path is empty")
	}
	return engineID, []string{path}, nil
}
