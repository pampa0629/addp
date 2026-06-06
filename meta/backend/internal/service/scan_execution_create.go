package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scantask"
)

func validateManualScanRequestTriggerType(triggerType string) error {
	normalized := strings.ToLower(strings.TrimSpace(triggerType))
	if normalized == "" {
		return nil
	}
	if normalized == models.TriggerTypeManual {
		return nil
	}
	return fmt.Errorf("unsupported trigger_type %q: use manual", triggerType)
}

// CreateManualRun 创建手动扫描执行并入队
func (s *ScanExecutionService) CreateManualRun(ctx context.Context, tenantID, userID uint, token string, req *models.ScanRequest) (*commonExecution.TaskExecution, error) {
	if req == nil {
		return nil, errors.New("请求不能为空")
	}
	if err := validateManualScanRequestTriggerType(req.TriggerType); err != nil {
		return nil, err
	}
	scope, err := s.scanService.ResolveScanScope(tenantID, scanflow.Options{
		EngineID:     req.EngineID,
		CatalogPaths: req.CatalogPaths,
		RefGroups:    req.RefGroups,
		NodeID:       req.NodeID,
		ItemID:       req.ItemID,
		Targets:      req.Targets,
		ScanDepth:    req.ScanDepth,
		Force:        req.Force,
		Source:       req.Source,
	})
	if err != nil {
		return nil, fmt.Errorf("解析扫描范围失败: %w", err)
	}

	resource, err := s.engineService.GetResourceByID(scope.EngineID, tenantID, token)
	if err != nil {
		return nil, fmt.Errorf("验证资源失败: %w", err)
	}

	execution := scantask.NewManualExecution(
		tenantID,
		userID,
		scope.EngineID,
		req.ItemID,
		scantask.NormalizeStorageType(resource.EngineType),
		scope.CatalogPaths,
		scope.RefGroups,
		scope.ScanDepth,
		scope.Force,
		scope.Source,
		token,
		time.Now(),
	)

	lockKey := ""
	lockAcquired := false
	if s.dedupService != nil {
		lockKey = s.dedupService.GenerateExecutionLockKey(tenantID, scope.EngineID, req.ItemID, scope.CatalogPaths, scope.RefGroups)
		acquired, err := s.dedupService.TryAcquireOwnedLock(ctx, lockKey, execution.ExecutionID, 2*time.Hour)
		if err != nil {
			s.log.Warn("标记扫描范围运行失败，将继续创建执行", "error", err, "lock_key", lockKey)
		} else if !acquired {
			return nil, fmt.Errorf("该扫描范围正在执行中，请稍后再试")
		} else {
			lockAcquired = true
		}
	}

	if err := s.taskExecutionRepo.Create(ctx, execution); err != nil {
		s.releaseExecutionLock(ctx, lockAcquired, lockKey, execution.ExecutionID, "创建执行失败后释放扫描范围锁失败", "execution_id", execution.ExecutionID)
		return nil, err
	}

	s.enqueueExecution(execution.ExecutionID)
	return execution, nil
}

func (s *ScanExecutionService) CreateUnscannedRuns(ctx context.Context, tenantID, userID uint, token string) ([]*commonExecution.TaskExecution, error) {
	resources, err := s.engineService.GetEnginesWithStats(tenantID)
	if err != nil {
		return nil, err
	}

	runs := make([]*commonExecution.TaskExecution, 0, len(resources))
	for _, resource := range resources {
		if resource == nil {
			continue
		}
		if resource.ScannedAt != "" && resource.UnscannedCatalogNodes <= 0 {
			continue
		}
		run, err := s.CreateManualRun(ctx, tenantID, userID, token, &models.ScanRequest{
			EngineID:  resource.EngineID,
			ScanDepth: scanflow.ScanDepthDeep,
			Force:     false,
			Source:    commonExecution.ModuleMeta,
		})
		if err != nil {
			s.log.Warn("未扫描引擎后台扫描运行创建失败，跳过该引擎",
				"engine_id", resource.EngineID,
				"engine_name", resource.ResourceName,
				"error", err,
			)
			continue
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// CreateTaskManualRun 基于已有扫描任务定义创建一次手动执行
func (s *ScanExecutionService) CreateTaskManualRun(ctx context.Context, task *models.ScanTask, userID uint) (*commonExecution.TaskExecution, error) {
	if task == nil {
		return nil, errors.New("扫描任务不能为空")
	}
	storageType := s.lookupStorageType(task.EngineID, task.TenantID)
	execution := scantask.NewTaskManualExecution(task, userID, storageType, time.Now())

	if err := s.taskExecutionRepo.Create(ctx, execution); err != nil {
		return nil, err
	}

	s.enqueueExecution(execution.ExecutionID)
	return execution, nil
}
