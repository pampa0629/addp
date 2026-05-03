package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	commonRepo "github.com/addp/common/repository"
	commonScheduler "github.com/addp/common/scheduler"
	"github.com/addp/meta/internal/models"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// TaskQueue 任务队列接口（避免循环依赖）
type TaskQueue interface {
	EnqueueScanTask(ctx context.Context, executionID string, taskID, tenantID uint) error
	Close() error
}

// ScanTaskService 管理扫描任务、队列与调度
type ScanTaskService struct {
	db                *gorm.DB
	scanService       *ScanService
	engineService     *EngineService
	dedupService      *ScanDedupService
	taskExecutionRepo *commonRepo.TaskExecutionRepository
	log               *slog.Logger
	taskQueue         TaskQueue

	// 本地队列（当 taskQueue 为 nil 时使用）
	queue   chan string // executionID (UUID)
	workers int
	stopCh  chan struct{}
	wg      sync.WaitGroup

	// 公共调度器
	scheduler    commonScheduler.Scheduler
	exprBuilder  *commonScheduler.ExpressionBuilder
	taskEntryIDs map[uint]string
	cronMu       sync.RWMutex
}

// NewScanTaskService 创建任务服务
func NewScanTaskService(db *gorm.DB, scanService *ScanService, engineService *EngineService, redisClient *redis.Client) *ScanTaskService {
	if scanService == nil {
		scanService = NewScanService(db, engineService)
	}

	var dedupService *ScanDedupService
	if redisClient != nil {
		dedupService = NewScanDedupService(redisClient)
		scanService.SetDedupService(dedupService)
	}

	scheduler, err := commonScheduler.NewScheduler(commonScheduler.Options{
		Name: "meta-scanner",
	})
	if err != nil {
		panic(fmt.Sprintf("failed to create scheduler: %v", err))
	}

	return &ScanTaskService{
		db:                db,
		scanService:       scanService,
		engineService:     engineService,
		dedupService:      dedupService,
		taskExecutionRepo: commonRepo.NewTaskExecutionRepository(db),
		log:               logger.With("component", "scan_task_service"),
		queue:             make(chan string, 128),
		workers:           2,
		stopCh:            make(chan struct{}),
		scheduler:         scheduler,
		exprBuilder:       commonScheduler.NewExpressionBuilder(),
		taskEntryIDs:      make(map[uint]string),
	}
}

// SetTaskQueue 设置任务队列（用于异步执行）
func (s *ScanTaskService) SetTaskQueue(queue TaskQueue) {
	s.taskQueue = queue
}

// Start 启动任务服务（队列消费者 + 定时调度）
func (s *ScanTaskService) Start(ctx context.Context) error {
	s.log.Info("启动扫描任务服务")

	if s.taskQueue == nil {
		for i := 0; i < s.workers; i++ {
			s.wg.Add(1)
			go s.workerLoop()
		}
	}

	if err := s.recoverPendingExecutions(); err != nil {
		s.log.Warn("恢复历史执行失败", "error", err)
	}

	if err := s.bootstrapSchedules(); err != nil {
		s.log.Warn("加载定时任务失败", "error", err)
	}

	s.scheduler.Start(ctx)
	return nil
}

// Stop 停止任务服务
func (s *ScanTaskService) Stop(ctx context.Context) {
	s.log.Info("停止扫描任务服务")
	close(s.stopCh)
	s.scheduler.Stop(ctx)
	s.wg.Wait()
	close(s.queue)
}

func (s *ScanTaskService) workerLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stopCh:
			return
		case executionID, ok := <-s.queue:
			if !ok {
				return
			}
			if err := s.executeRun(context.Background(), executionID); err != nil {
				s.log.Error("任务执行失败", "execution_id", executionID, "error", err)
			}
		}
	}
}

func (s *ScanTaskService) recoverPendingExecutions() error {
	ctx := context.Background()
	// 查询 meta 模块下所有 pending/running 的执行记录（0 = 不按租户过滤）
	executions, err := s.taskExecutionRepo.GetRunningExecutions(ctx, 0)
	if err != nil {
		return err
	}

	for _, exec := range executions {
		if exec.Module != commonModels.ModuleMeta {
			continue
		}
		if exec.Status == commonModels.ExecutionStatusRunning {
			if err := s.taskExecutionRepo.UpdateFields(ctx, exec.ExecutionID, exec.TenantID, map[string]interface{}{
				"status":       commonModels.ExecutionStatusPending,
				"current_step": "检测到未完成执行，已重新排队",
				"updated_at":   time.Now(),
			}); err != nil {
				s.log.Warn("重置执行状态失败", "execution_id", exec.ExecutionID, "error", err)
				continue
			}
		}
		s.enqueueExecution(exec.ExecutionID)
	}
	return nil
}

func (s *ScanTaskService) bootstrapSchedules() error {
	var tasks []models.ScanTask
	if err := s.db.Where("enabled = ?", true).Find(&tasks).Error; err != nil {
		return err
	}

	for i := range tasks {
		task := tasks[i]
		if task.Schedule == "" {
			continue
		}
		if err := s.scheduleTask(&task); err != nil {
			s.log.Warn("调度任务失败", "task_id", task.ID, "error", err)
		}
	}
	return nil
}

func (s *ScanTaskService) scheduleTask(task *models.ScanTask) error {
	if task == nil {
		return errors.New("nil task")
	}
	if task.Schedule == "" || !task.Enabled {
		return nil
	}

	s.cronMu.Lock()
	defer s.cronMu.Unlock()

	ctx := context.Background()
	taskID := fmt.Sprintf("%d", task.ID)

	handler := func(ctx context.Context, id string) error {
		if err := s.triggerScheduledTask(task.ID); err != nil {
			s.log.Error("定时任务触发失败", "task_id", task.ID, "error", err)
			return err
		}
		return nil
	}

	if err := s.scheduler.Schedule(ctx, taskID, task.Schedule, handler); err != nil {
		return err
	}

	s.taskEntryIDs[task.ID] = taskID
	return nil
}

func (s *ScanTaskService) unscheduleTask(taskID uint) {
	s.cronMu.Lock()
	defer s.cronMu.Unlock()

	ctx := context.Background()
	taskIDStr := fmt.Sprintf("%d", taskID)
	s.scheduler.Unschedule(ctx, taskIDStr)
	delete(s.taskEntryIDs, taskID)
}

func (s *ScanTaskService) enqueueExecution(executionID string) {
	if s.taskQueue != nil {
		ctx := context.Background()
		exec, err := s.taskExecutionRepo.GetByExecutionID(ctx, executionID, 0)
		if err != nil {
			s.log.Error("获取执行信息失败", "execution_id", executionID, "error", err)
			return
		}

		var taskID uint
		if exec.SourceTaskID != nil {
			taskID = uint(*exec.SourceTaskID)
		}

		if err := s.taskQueue.EnqueueScanTask(ctx, executionID, taskID, uint(exec.TenantID)); err != nil {
			s.log.Error("任务入队失败", "execution_id", executionID, "error", err)
			_ = s.taskExecutionRepo.UpdateFields(ctx, executionID, exec.TenantID, map[string]interface{}{
				"status": commonModels.ExecutionStatusFailed,
				"error_details": commonModels.JSONMap{
					"message": fmt.Sprintf("任务入队失败: %v", err),
				},
				"updated_at": time.Now(),
			})
		}
		return
	}

	select {
	case s.queue <- executionID:
	default:
		s.queue <- executionID
	}
}

// CreateManualRun 创建手动扫描执行并入队
func (s *ScanTaskService) CreateManualRun(ctx context.Context, tenantID, userID uint, token string, req *models.ScanRequest) (*commonModels.TaskExecution, error) {
	if req == nil {
		return nil, errors.New("请求不能为空")
	}
	if req.EngineID == 0 {
		return nil, errors.New("engine_id 不能为空")
	}

	resource, err := s.engineService.GetResourceByID(req.EngineID, tenantID, token)
	if err != nil {
		return nil, fmt.Errorf("验证资源失败: %w", err)
	}

	if s.dedupService != nil {
		taskKey := s.dedupService.GenerateTaskKey(tenantID, req.EngineID, models.TriggerTypeManual)
		if s.dedupService.CheckTaskExists(ctx, taskKey) {
			return nil, fmt.Errorf("该资源正在扫描中，请稍后再试")
		}
		if err := s.dedupService.MarkTaskRunning(ctx, taskKey, 2*time.Hour); err != nil {
			s.log.Warn("标记任务运行失败", "error", err)
		}
	}

	startTime := time.Now()
	userIDInt := int(userID)
	execution := &commonModels.TaskExecution{
		TenantID:    int(tenantID),
		ExecutionID: uuid.New().String(),
		Module:      commonModels.ModuleMeta,
		TaskType:    "scan",
		Status:      commonModels.ExecutionStatusPending,
		TriggerType: commonModels.TriggerTypeManual,
		TriggeredBy: &userIDInt,
		ExecutionConfig: commonModels.JSONMap{
			"engine_id":    req.EngineID,
			"storage_type": normalizeStorageType(resource.EngineType),
			"namespaces":   req.Namespaces,
			"object_paths": req.ObjectPaths,
			"scan_depth":   req.ScanDepth,
			"token":        token,
		},
		StartedAt: &startTime,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.taskExecutionRepo.Create(ctx, execution); err != nil {
		return nil, err
	}

	s.enqueueExecution(execution.ExecutionID)
	return execution, nil
}

// ExecuteScanRun 执行扫描（供 Worker 调用）
func (s *ScanTaskService) ExecuteScanRun(ctx context.Context, executionID string) error {
	return s.executeRun(ctx, executionID)
}

func (s *ScanTaskService) executeRun(ctx context.Context, executionID string) error {
	exec, err := s.taskExecutionRepo.GetByExecutionID(ctx, executionID, 0)
	if err != nil {
		return err
	}

	// 从 ExecutionConfig 中恢复参数
	var engineID uint
	var namespaces, objectPaths []string
	var scanDepth, token, storageType string

	if exec.ExecutionConfig != nil {
		if v, ok := exec.ExecutionConfig["engine_id"]; ok {
			switch val := v.(type) {
			case float64:
				engineID = uint(val)
			case int:
				engineID = uint(val)
			}
		}
		storageType, _ = exec.ExecutionConfig["storage_type"].(string)
		scanDepth, _ = exec.ExecutionConfig["scan_depth"].(string)
		token, _ = exec.ExecutionConfig["token"].(string)
		namespaces = extractSliceFromInterface(exec.ExecutionConfig["namespaces"])
		objectPaths = extractSliceFromInterface(exec.ExecutionConfig["object_paths"])
	}

	if engineID == 0 {
		return fmt.Errorf("执行配置缺少 engine_id: execution_id=%s", executionID)
	}

	// 任务完成后清理去重标记
	defer func() {
		if s.dedupService != nil {
			taskKey := s.dedupService.GenerateTaskKey(uint(exec.TenantID), engineID, exec.TriggerType)
			if err := s.dedupService.ClearTask(context.Background(), taskKey); err != nil {
				s.log.Warn("清除任务标记失败", "execution_id", executionID, "error", err)
			}
			if err := s.dedupService.UpdateLastScanTime(context.Background(), engineID); err != nil {
				s.log.Warn("更新最后扫描时间失败", "execution_id", executionID, "error", err)
			}
		}
	}()

	if exec.Status != commonModels.ExecutionStatusPending {
		s.log.Info("跳过非待执行任务", "execution_id", executionID, "status", exec.Status)
		return nil
	}

	start := time.Now()
	if err := s.taskExecutionRepo.UpdateFields(ctx, executionID, exec.TenantID, map[string]interface{}{
		"status":       commonModels.ExecutionStatusRunning,
		"started_at":   start,
		"current_step": "任务开始执行",
		"updated_at":   time.Now(),
	}); err != nil {
		return err
	}

	if scanDepth == "" {
		scanDepth = "deep"
	}

	reporter := newExecProgressReporter(s, executionID, exec.TenantID)
	reporter.Message("任务开始执行")

	resp, scanErr := s.scanService.ScanEngineWithDepth(engineID, uint(exec.TenantID), namespaces, objectPaths, token, scanDepth, reporter)
	completeTime := time.Now()
	durationMs := completeTime.Sub(start).Milliseconds()

	if scanErr != nil {
		_ = s.taskExecutionRepo.UpdateFields(ctx, executionID, exec.TenantID, map[string]interface{}{
			"status":            commonModels.ExecutionStatusFailed,
			"completed_at":      completeTime,
			"execution_time_ms": durationMs,
			"current_step":      fmt.Sprintf("执行失败: %v", scanErr),
			"error_details": commonModels.JSONMap{
				"message": scanErr.Error(),
			},
			"updated_at": time.Now(),
		})

		if exec.SourceTaskID != nil {
			s.backfillTaskStatus(uint(*exec.SourceTaskID), executionID, commonModels.ExecutionStatusFailed, completeTime, exec.TenantID)
		}
		return scanErr
	}

	metadata := commonModels.JSONMap{
		"namespaces_scanned": resp.NamespacesScanned,
		"items_scanned":      resp.ItemsScanned,
		"fields_scanned":     resp.FieldsScanned,
		"storage_type":       storageType,
	}

	_ = s.taskExecutionRepo.UpdateFields(ctx, executionID, exec.TenantID, map[string]interface{}{
		"status":            commonModels.ExecutionStatusSuccess,
		"completed_at":      completeTime,
		"execution_time_ms": durationMs,
		"progress":          100,
		"current_step":      "执行完成",
		"metadata":          metadata,
		"updated_at":        time.Now(),
	})

	if exec.SourceTaskID != nil {
		s.backfillTaskStatus(uint(*exec.SourceTaskID), executionID, commonModels.ExecutionStatusSuccess, completeTime, exec.TenantID)
	}

	return nil
}

// backfillTaskStatus 回写 ScanTask 的最近执行状态字段
func (s *ScanTaskService) backfillTaskStatus(taskID uint, executionID string, status string, completedAt time.Time, tenantID int) {
	next := s.computeNextRunTime(taskID)
	taskUpdate := map[string]interface{}{
		"last_run_at":           completedAt,
		"last_execution_id":     executionID,
		"last_execution_status": status,
		"updated_at":            time.Now(),
	}
	if next != nil {
		taskUpdate["next_run_at"] = *next
	}
	if err := s.db.Model(&models.ScanTask{}).Where("id = ? AND tenant_id = ?", taskID, tenantID).Updates(taskUpdate).Error; err != nil {
		s.log.Warn("更新任务执行状态失败", "task_id", taskID, "error", err)
	}
}

func (s *ScanTaskService) updateExecutionProgress(executionID string, tenantID int, fields map[string]interface{}) {
	fields["updated_at"] = time.Now()
	if err := s.taskExecutionRepo.UpdateFields(context.Background(), executionID, tenantID, fields); err != nil {
		s.log.Warn("更新执行进度失败", "execution_id", executionID, "error", err)
	}
}

// GetExecution 获取执行详情
func (s *ScanTaskService) GetExecution(ctx context.Context, executionID string, tenantID int) (*commonModels.TaskExecution, error) {
	return s.taskExecutionRepo.GetByExecutionID(ctx, executionID, tenantID)
}

// ListExecutions 列出 meta 模块的执行记录
func (s *ScanTaskService) ListExecutions(ctx context.Context, tenantID int, taskID *int, status, triggerType string, page, pageSize int) ([]*commonModels.TaskExecution, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	filter := commonRepo.TaskExecutionFilter{
		TenantID:     tenantID,
		Module:       commonModels.ModuleMeta,
		Status:       status,
		TriggerType:  triggerType,
		SourceTaskID: taskID,
		Page:         page,
		PageSize:     pageSize,
	}
	return s.taskExecutionRepo.List(ctx, filter)
}

// CancelExecution 取消执行
func (s *ScanTaskService) CancelExecution(ctx context.Context, executionID string, tenantID int) error {
	exec, err := s.taskExecutionRepo.GetByExecutionID(ctx, executionID, tenantID)
	if err != nil {
		return err
	}
	if exec.IsCompleted() {
		return fmt.Errorf("执行已完成，无法取消: status=%s", exec.Status)
	}
	return s.taskExecutionRepo.UpdateFields(ctx, executionID, tenantID, map[string]interface{}{
		"status":     commonModels.ExecutionStatusCancelled,
		"updated_at": time.Now(),
	})
}

// GetTask 获取单个任务
func (s *ScanTaskService) GetTask(tenantID, taskID uint) (*models.ScanTask, error) {
	var task models.ScanTask
	if err := s.db.Where("tenant_id = ?", tenantID).First(&task, taskID).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// ListTasks 获取租户下的任务列表
func (s *ScanTaskService) ListTasks(tenantID uint) ([]models.ScanTask, error) {
	var tasks []models.ScanTask
	if err := s.db.Where("tenant_id = ?", tenantID).
		Order("created_at DESC").
		Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

// CreateTask 创建新的扫描任务
func (s *ScanTaskService) CreateTask(ctx context.Context, tenantID, userID uint, req *models.ScanTaskUpsertRequest) (*models.ScanTask, error) {
	if req == nil {
		return nil, errors.New("请求不能为空")
	}
	if req.EngineID == 0 {
		return nil, errors.New("engine_id 不能为空")
	}
	if req.Name == "" {
		return nil, errors.New("任务名称不能为空")
	}

	params := models.JSONMap{
		"namespaces":   req.Namespaces,
		"object_paths": req.ObjectPaths,
		"scan_depth":   req.ScanDepth,
	}

	task := &models.ScanTask{
		TenantID:    tenantID,
		EngineID:    req.EngineID,
		Name:        req.Name,
		Description: req.Description,
		Schedule:    req.Schedule,
		Enabled:     req.Enabled,
		Parameters:  params,
		CreatedBy:   userID,
		UpdatedBy:   userID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if req.Schedule != "" {
		if next := s.nextTimeFromSpec(req.Schedule, time.Now()); next != nil {
			task.NextRunAt = next
		}
	}

	if err := s.db.Create(task).Error; err != nil {
		return nil, err
	}

	if task.Schedule != "" && task.Enabled {
		if err := s.scheduleTask(task); err != nil {
			s.log.Warn("任务调度失败", "task_id", task.ID, "error", err)
		}
	}

	return task, nil
}

// UpdateTask 更新任务配置
func (s *ScanTaskService) UpdateTask(ctx context.Context, tenantID, taskID, userID uint, req *models.ScanTaskUpsertRequest) (*models.ScanTask, error) {
	task, err := s.GetTask(tenantID, taskID)
	if err != nil {
		return nil, err
	}

	params := models.JSONMap{
		"namespaces":   req.Namespaces,
		"object_paths": req.ObjectPaths,
		"scan_depth":   req.ScanDepth,
	}

	task.Name = req.Name
	task.Description = req.Description
	task.EngineID = req.EngineID
	task.Schedule = req.Schedule
	task.Enabled = req.Enabled
	task.Parameters = params
	task.UpdatedBy = userID
	task.UpdatedAt = time.Now()

	if req.Schedule != "" {
		task.NextRunAt = s.nextTimeFromSpec(req.Schedule, time.Now())
	} else {
		task.NextRunAt = nil
	}

	if err := s.db.Save(task).Error; err != nil {
		return nil, err
	}

	if task.Schedule != "" && task.Enabled {
		if err := s.scheduleTask(task); err != nil {
			s.log.Warn("更新后任务调度失败", "task_id", task.ID, "error", err)
		}
	} else {
		s.unscheduleTask(task.ID)
	}

	return task, nil
}

// DeleteTask 删除任务
func (s *ScanTaskService) DeleteTask(ctx context.Context, tenantID, taskID uint) error {
	if _, err := s.GetTask(tenantID, taskID); err != nil {
		return err
	}
	s.unscheduleTask(taskID)
	return s.db.Delete(&models.ScanTask{}, taskID).Error
}

// TriggerTaskNow 立即触发任务执行
func (s *ScanTaskService) TriggerTaskNow(ctx context.Context, tenantID, taskID, userID uint) (*commonModels.TaskExecution, error) {
	task, err := s.GetTask(tenantID, taskID)
	if err != nil {
		return nil, err
	}

	storageType := s.lookupStorageType(task.EngineID, task.TenantID)

	startTime := time.Now()
	userIDInt := int(userID)
	taskIDInt := int(taskID)

	execution := &commonModels.TaskExecution{
		TenantID:       int(tenantID),
		ExecutionID:    uuid.New().String(),
		Module:         commonModels.ModuleMeta,
		TaskType:       "scan",
		SourceTaskID:   &taskIDInt,
		SourceTaskName: &task.Name,
		Status:         commonModels.ExecutionStatusPending,
		TriggerType:    commonModels.TriggerTypeManual,
		TriggeredBy:    &userIDInt,
		ExecutionConfig: commonModels.JSONMap{
			"engine_id":    task.EngineID,
			"storage_type": storageType,
			"namespaces":   extractJSONMapSlice(task.Parameters, "namespaces"),
			"object_paths": extractJSONMapSlice(task.Parameters, "object_paths"),
			"scan_depth":   extractJSONMapString(task.Parameters, "scan_depth", "deep"),
		},
		StartedAt: &startTime,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.taskExecutionRepo.Create(ctx, execution); err != nil {
		return nil, err
	}

	s.enqueueExecution(execution.ExecutionID)
	return execution, nil
}

func (s *ScanTaskService) triggerScheduledTask(taskID uint) error {
	ctx := context.Background()
	var task models.ScanTask
	if err := s.db.First(&task, taskID).Error; err != nil {
		return err
	}
	if !task.Enabled {
		return nil
	}

	if s.dedupService != nil {
		taskKey := s.dedupService.GenerateTaskKey(task.TenantID, task.EngineID, models.TriggerTypeScheduled)
		if s.dedupService.CheckTaskExists(ctx, taskKey) {
			s.log.Info("该资源正在扫描中，跳过本次定时触发", "task_id", taskID)
			return nil
		}

		lastScan, err := s.dedupService.GetLastScanTime(ctx, task.EngineID)
		if err == nil && lastScan != nil {
			if time.Since(*lastScan) < 6*time.Hour {
				s.log.Info("距离上次扫描不足 6 小时，跳过本次定时触发",
					"task_id", taskID,
					"last_scan", lastScan,
					"since", time.Since(*lastScan))
				return nil
			}
		}

		if err := s.dedupService.MarkTaskRunning(ctx, taskKey, 2*time.Hour); err != nil {
			s.log.Warn("标记任务运行失败", "error", err)
		}
	}

	targetSchemas, targetPaths := s.computeInheritedTargets(&task)

	if len(targetSchemas) == 0 && len(targetPaths) == 0 {
		s.log.Info("所有目标均已配置独立调度，跳过引擎级扫描",
			"task_id", taskID,
			"engine_id", task.EngineID)
		return nil
	}

	storageType := s.lookupStorageType(task.EngineID, task.TenantID)

	startTime := time.Now()
	taskIDInt := int(taskID)

	execution := &commonModels.TaskExecution{
		TenantID:       int(task.TenantID),
		ExecutionID:    uuid.New().String(),
		Module:         commonModels.ModuleMeta,
		TaskType:       "scan",
		SourceTaskID:   &taskIDInt,
		SourceTaskName: &task.Name,
		Status:         commonModels.ExecutionStatusPending,
		TriggerType:    commonModels.TriggerTypeSchedule,
		ExecutionConfig: commonModels.JSONMap{
			"engine_id":    task.EngineID,
			"storage_type": storageType,
			"namespaces":   targetSchemas,
			"object_paths": targetPaths,
			"scan_depth":   "deep",
		},
		StartedAt: &startTime,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.taskExecutionRepo.Create(ctx, execution); err != nil {
		return err
	}

	now := time.Now()
	next := s.nextTimeFromSpec(task.Schedule, now)
	taskUpdate := map[string]interface{}{
		"last_run_at": now,
		"updated_at":  now,
	}
	if next != nil {
		taskUpdate["next_run_at"] = *next
	}
	if err := s.db.Model(&models.ScanTask{}).Where("id = ?", task.ID).Updates(taskUpdate).Error; err != nil {
		s.log.Warn("更新任务定时信息失败", "task_id", task.ID, "error", err)
	}

	s.enqueueExecution(execution.ExecutionID)
	return nil
}

// computeInheritedTargets 计算继承目标（排除已有独立调度的schema/bucket）
func (s *ScanTaskService) computeInheritedTargets(task *models.ScanTask) ([]string, []string) {
	if task == nil || task.Parameters == nil {
		return []string{}, []string{}
	}

	allSchemas := extractJSONMapSlice(task.Parameters, "namespaces")
	allPaths := extractJSONMapSlice(task.Parameters, "object_paths")

	var independentTasks []models.ScanTask
	if err := s.db.Where("engine_id = ? AND id != ? AND enabled = ?",
		task.EngineID, task.ID, true).Find(&independentTasks).Error; err != nil {
		s.log.Warn("查询独立调度任务失败", "engine_id", task.EngineID, "error", err)
		return allSchemas, allPaths
	}

	scheduledSchemas := make(map[string]bool)
	scheduledPaths := make(map[string]bool)

	for _, t := range independentTasks {
		for _, s := range extractJSONMapSlice(t.Parameters, "namespaces") {
			scheduledSchemas[s] = true
		}
		for _, p := range extractJSONMapSlice(t.Parameters, "object_paths") {
			scheduledPaths[p] = true
		}
	}

	targetSchemas := []string{}
	for _, schemaName := range allSchemas {
		if !scheduledSchemas[schemaName] {
			targetSchemas = append(targetSchemas, schemaName)
		}
	}

	targetPaths := []string{}
	for _, pathStr := range allPaths {
		if !scheduledPaths[pathStr] {
			targetPaths = append(targetPaths, pathStr)
		}
	}

	return targetSchemas, targetPaths
}

func normalizeStorageType(resourceType string) string {
	normalized := strings.ToLower(strings.TrimSpace(resourceType))
	if normalized == "" {
		return "unknown"
	}
	return strings.ReplaceAll(normalized, " ", "_")
}

func (s *ScanTaskService) lookupStorageType(engineID, tenantID uint) string {
	resource, err := s.engineService.GetResourceByID(engineID, tenantID, "")
	if err != nil {
		s.log.Warn("获取资源存储类型失败", "engine_id", engineID, "tenant_id", tenantID, "error", err)
		return "unknown"
	}
	return normalizeStorageType(resource.EngineType)
}

func (s *ScanTaskService) computeNextRunTime(taskID uint) *time.Time {
	var task models.ScanTask
	if err := s.db.Select("schedule").First(&task, taskID).Error; err != nil {
		return nil
	}
	return s.nextTimeFromSpec(task.Schedule, time.Now())
}

func (s *ScanTaskService) nextTimeFromSpec(spec string, from time.Time) *time.Time {
	if spec == "" {
		return nil
	}
	next, err := s.exprBuilder.NextRunTime(spec, from)
	if err != nil {
		s.log.Warn("解析 Cron 表达式失败", "spec", spec, "error", err)
		return nil
	}
	return &next
}

// CreateOrUpdateTaskFromScanConfig 根据资源的扫描配置创建或更新自动扫描任务
func (s *ScanTaskService) CreateOrUpdateTaskFromScanConfig(resource *commonModels.Engine) error {
	if resource == nil || resource.ScanConfig == nil || !resource.ScanConfig.Enabled {
		return nil
	}

	scanConfig := resource.ScanConfig

	var tenantID uint
	if resource.TenantID != nil {
		tenantID = *resource.TenantID
	}

	var existingTask models.ScanTask
	err := s.db.Where("engine_id = ? AND tenant_id = ? AND name LIKE ?",
		resource.ID, tenantID, "自动扫描%").First(&existingTask).Error

	taskName := fmt.Sprintf("自动扫描 - %s", resource.Name)

	cronExpr, err := s.buildCronExpressionFromScanConfig(scanConfig)
	if err != nil {
		return fmt.Errorf("构建 Cron 表达式失败: %w", err)
	}

	parameters := models.JSONMap{
		"scan_depth": "deep",
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		task := &models.ScanTask{
			TenantID:    tenantID,
			EngineID:    resource.ID,
			Name:        taskName,
			Description: "由存储引擎注册时自动创建",
			Schedule:    cronExpr,
			Enabled:     true,
			Parameters:  parameters,
		}

		if err := s.db.Create(task).Error; err != nil {
			return fmt.Errorf("创建自动扫描任务失败: %w", err)
		}

		if err := s.scheduleTask(task); err != nil {
			s.log.Warn("调度任务失败", "task_id", task.ID, "error", err)
		}

		s.log.Info("自动扫描任务已创建",
			"task_id", task.ID,
			"engine_id", resource.ID,
			"resource_name", resource.Name)

		return nil
	} else if err != nil {
		return fmt.Errorf("查询已有任务失败: %w", err)
	}

	updates := map[string]interface{}{
		"name":       taskName,
		"schedule":   cronExpr,
		"enabled":    scanConfig.Enabled,
		"parameters": parameters,
		"updated_at": time.Now(),
	}

	if err := s.db.Model(&existingTask).Updates(updates).Error; err != nil {
		return fmt.Errorf("更新自动扫描任务失败: %w", err)
	}

	s.unscheduleTask(existingTask.ID)
	updatedTask := existingTask
	updatedTask.Name = taskName
	updatedTask.Schedule = cronExpr
	updatedTask.Enabled = scanConfig.Enabled
	updatedTask.Parameters = parameters

	if err := s.scheduleTask(&updatedTask); err != nil {
		s.log.Warn("重新调度任务失败", "task_id", updatedTask.ID, "error", err)
	}

	s.log.Info("自动扫描任务已更新",
		"task_id", existingTask.ID,
		"engine_id", resource.ID,
		"resource_name", resource.Name)

	return nil
}

// DeleteTaskByResourceID 删除指定资源关联的所有自动扫描任务
func (s *ScanTaskService) DeleteTaskByResourceID(engineID uint) error {
	var tasks []models.ScanTask
	if err := s.db.Where("engine_id = ? AND name LIKE ?", engineID, "自动扫描%").Find(&tasks).Error; err != nil {
		return fmt.Errorf("查询资源关联任务失败: %w", err)
	}

	for _, task := range tasks {
		s.unscheduleTask(task.ID)
		if err := s.db.Delete(&task).Error; err != nil {
			s.log.Warn("删除任务失败", "task_id", task.ID, "error", err)
		}
	}

	return nil
}

func parseScheduleTime(scheduleTime string) (hour, minute int) {
	if scheduleTime == "" {
		return 0, 0
	}
	parts := strings.Split(scheduleTime, ":")
	if len(parts) == 2 {
		if h, err := strconv.Atoi(parts[0]); err == nil {
			hour = h
		}
		if m, err := strconv.Atoi(parts[1]); err == nil {
			minute = m
		}
	}
	return hour, minute
}

func (s *ScanTaskService) buildCronExpressionFromScanConfig(config *commonModels.ScanConfig) (string, error) {
	if config == nil {
		return "", errors.New("扫描配置为空")
	}

	switch config.ScheduleType {
	case "manual":
		return "", nil
	case "cron":
		if config.CronExpression == "" {
			return "", errors.New("Cron 类型必须提供 cron_expression")
		}
		if err := s.exprBuilder.Validate(config.CronExpression); err != nil {
			return "", fmt.Errorf("无效的 Cron 表达式: %w", err)
		}
		return config.CronExpression, nil
	case "daily":
		hour, minute := parseScheduleTime(config.ScheduleTime)
		return fmt.Sprintf("%d %d * * *", minute, hour), nil
	case "weekly":
		hour, minute := parseScheduleTime(config.ScheduleTime)
		days := "0"
		if len(config.ScheduleValue) > 0 {
			dayStrs := make([]string, len(config.ScheduleValue))
			for i, day := range config.ScheduleValue {
				dayStrs[i] = strconv.Itoa(day)
			}
			days = strings.Join(dayStrs, ",")
		}
		return fmt.Sprintf("%d %d * * %s", minute, hour, days), nil
	case "monthly":
		hour, minute := parseScheduleTime(config.ScheduleTime)
		dates := "1"
		if len(config.ScheduleValue) > 0 {
			dateStrs := make([]string, len(config.ScheduleValue))
			for i, date := range config.ScheduleValue {
				dateStrs[i] = strconv.Itoa(date)
			}
			dates = strings.Join(dateStrs, ",")
		}
		return fmt.Sprintf("%d %d %s * *", minute, hour, dates), nil
	default:
		return "", fmt.Errorf("不支持的调度类型: %s", config.ScheduleType)
	}
}

// execProgressReporter 将扫描进度写入 common.task_executions
type execProgressReporter struct {
	service     *ScanTaskService
	executionID string
	tenantID    int

	total         int
	mu            sync.Mutex
	stats         map[string]int64
	lastFlushTime time.Time
	updateCount   int
}

func newExecProgressReporter(service *ScanTaskService, executionID string, tenantID int) *execProgressReporter {
	return &execProgressReporter{
		service:       service,
		executionID:   executionID,
		tenantID:      tenantID,
		stats:         make(map[string]int64),
		lastFlushTime: time.Now(),
	}
}

func (r *execProgressReporter) SetTotal(total int) {
	r.mu.Lock()
	r.total = total
	r.mu.Unlock()
}

func (r *execProgressReporter) Advance(label string, completed, total int, meta map[string]interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.total = total
	for k, v := range meta {
		switch val := v.(type) {
		case int:
			r.stats[k] += int64(val)
		case int64:
			r.stats[k] += val
		case float64:
			r.stats[k] += int64(val)
		}
	}

	r.updateCount++

	shouldFlush := completed == total ||
		time.Since(r.lastFlushTime) >= 5*time.Second ||
		r.updateCount >= 5

	if shouldFlush {
		progress := calcProgressPercent(completed, total)
		currentStep := fmt.Sprintf("已完成 %d/%d，最新完成: %s", completed, total, label)

		statsClone := make(commonModels.JSONMap)
		for k, v := range r.stats {
			statsClone[k] = v
		}

		r.mu.Unlock()
		r.service.updateExecutionProgress(r.executionID, r.tenantID, map[string]interface{}{
			"progress":     int(progress),
			"current_step": currentStep,
			"metadata":     statsClone,
		})
		r.mu.Lock()
		r.lastFlushTime = time.Now()
		r.updateCount = 0
	}
}

func (r *execProgressReporter) Message(message string) {
	r.service.updateExecutionProgress(r.executionID, r.tenantID, map[string]interface{}{
		"current_step": message,
	})
}

func calcProgressPercent(done, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(done) * 100.0 / float64(total)
}

// extractJSONMapSlice 从 JSONMap 中提取字符串数组
func extractJSONMapSlice(m models.JSONMap, key string) []string {
	if m == nil {
		return nil
	}
	raw, ok := m[key]
	if !ok {
		return nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// extractJSONMapString 从 JSONMap 中提取字符串，缺失时返回默认值
func extractJSONMapString(m models.JSONMap, key, defaultVal string) string {
	if m == nil {
		return defaultVal
	}
	v, ok := m[key]
	if !ok {
		return defaultVal
	}
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return defaultVal
}

// extractSliceFromInterface 从 interface{} 中提取字符串数组
func extractSliceFromInterface(raw interface{}) []string {
	if raw == nil {
		return nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func cloneJSONMap(m models.JSONMap) models.JSONMap {
	if m == nil {
		return nil
	}
	clone := models.JSONMap{}
	for k, v := range m {
		clone[k] = v
	}
	return clone
}
