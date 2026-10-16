package continuous

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	engineplugin "github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/resourcetree"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
)

type PreparedTargetMetadataScanner interface {
	ScanPreparedTarget(ctx context.Context, claim repository.RuntimeLeaseClaim, plan *planner.ContinuousPlan) error
}

type InitialMetadataScanStore interface {
	ClaimInitialMetadataScan(ctx context.Context, claim repository.RuntimeLeaseClaim, targetLocator string, now time.Time, claimTTL time.Duration) (*models.TransferTask, bool, error)
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
	engineID, targets, err := preparedTargetMetadataScope(plan)
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
	task, owned, err := s.Store.ClaimInitialMetadataScan(ctx, claim, targets[0], now(), claimTTL)
	if err != nil || !owned {
		return err
	}
	claimToken := task.InitialMetadataScanClaimToken
	run, scanErr := s.Client.WithTenantID(task.TenantID).CreateManualScanRun(commonClient.MetaScanOptions{
		EngineID: engineID, Targets: targets, ParentExecutionID: claim.Execution.ExecutionID,
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
				"error", scanErr, "task_id", task.ID, "engine_id", engineID, "targets", targets, "owned", completed)
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
			"task_id", task.ID, "engine_id", engineID, "targets", targets,
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
	if plan.Target.Path.EngineID != 0 && plan.Target.Path.EngineID != engineID {
		return 0, nil, fmt.Errorf("continuous target metadata scan engine ID mismatch")
	}
	segments := plan.Target.Path.Segments
	if len(segments) > 0 && engineplugin.IsEngineCatalogRootSegment(segments[0]) {
		segments = segments[1:]
	}
	if len(segments) < 2 || segments[len(segments)-1].Kind != engineplugin.EngineCatalogKindTable {
		return 0, nil, fmt.Errorf("continuous target metadata scan requires a target table path")
	}
	path := make([]string, 0, len(segments))
	for _, segment := range segments {
		if strings.TrimSpace(segment.Name) == "" {
			return 0, nil, fmt.Errorf("continuous target metadata scan path contains an empty name")
		}
		path = append(path, segment.Name)
	}
	target := (&resourcetree.ResourceLocator{EngineID: engineID, Path: path, Type: resourcetree.TypeTable}).ToURI()
	return engineID, []string{target}, nil
}
