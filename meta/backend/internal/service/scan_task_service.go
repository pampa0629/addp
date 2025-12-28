package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	commonScheduler "github.com/addp/common/scheduler"
	"github.com/addp/meta/internal/models"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// TaskQueue 任务队列接口（避免循环依赖）
type TaskQueue interface {
	EnqueueScanTask(ctx context.Context, runID, taskID, tenantID uint) error
	Close() error
}

// ScanTaskService 管理扫描任务、运行队列与调度
type ScanTaskService struct {
	db              *gorm.DB
	scanService     *ScanServiceNew
	resourceService *ResourceService
	dedupService    *ScanDedupService // 扫描去重服务
	log             *slog.Logger
	taskQueue       TaskQueue // 任务队列（可选,用于异步执行）

	// 本地队列（当 taskQueue 为 nil 时使用）
	queue   chan uint
	workers int
	stopCh  chan struct{}
	wg      sync.WaitGroup

	// 公共调度器
	scheduler    commonScheduler.Scheduler
	exprBuilder  *commonScheduler.ExpressionBuilder
	taskEntryIDs map[uint]string // 任务 ID 映射（改为 string）
	cronMu       sync.RWMutex
}

// ListRunsOptions 定义任务运行查询参数
type ListRunsOptions struct {
	TaskID        *uint
	ResourceID    *uint
	Status        string
	TriggerType   string
	StorageType   string
	StartedAfter  *time.Time
	StartedBefore *time.Time
	Limit         int
	Offset        int
}

// NewScanTaskService 创建任务服务
func NewScanTaskService(db *gorm.DB, scanService *ScanServiceNew, resourceService *ResourceService, redisClient *redis.Client) *ScanTaskService {
	if scanService == nil {
		scanService = NewScanServiceNew(db, resourceService)
	}

	var dedupService *ScanDedupService
	if redisClient != nil {
		dedupService = NewScanDedupService(redisClient)
	}

	// 创建公共调度器
	scheduler, err := commonScheduler.NewScheduler(commonScheduler.Options{
		Name: "meta-scanner",
	})
	if err != nil {
		panic(fmt.Sprintf("failed to create scheduler: %v", err))
	}

	return &ScanTaskService{
		db:              db,
		scanService:     scanService,
		resourceService: resourceService,
		dedupService:    dedupService,
		log:             logger.With("component", "scan_task_service"),
		queue:           make(chan uint, 128),
		workers:         2,
		stopCh:          make(chan struct{}),
		scheduler:       scheduler,
		exprBuilder:     commonScheduler.NewExpressionBuilder(),
		taskEntryIDs:    make(map[uint]string),
	}
}

// SetTaskQueue 设置任务队列（用于异步执行）
func (s *ScanTaskService) SetTaskQueue(queue TaskQueue) {
	s.taskQueue = queue
}

// Start 启动任务服务（队列消费者 + 定时调度）
// 如果配置了 taskQueue，则只启动定时调度，不启动本地 worker
func (s *ScanTaskService) Start(ctx context.Context) error {
	s.log.Info("启动扫描任务服务")

	// 如果没有配置任务队列，启动本地工作协程
	if s.taskQueue == nil {
		for i := 0; i < s.workers; i++ {
			s.wg.Add(1)
			go s.workerLoop()
		}
	}

	// 恢复未完成的运行
	if err := s.recoverPendingRuns(); err != nil {
		s.log.Warn("恢复历史运行失败", "error", err)
	}

	// 加载并调度定时任务
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
		case runID, ok := <-s.queue:
			if !ok {
				return
			}
			if err := s.executeRun(context.Background(), runID); err != nil {
				s.log.Error("任务运行失败", "run_id", runID, "error", err)
			}
		}
	}
}

func (s *ScanTaskService) recoverPendingRuns() error {
	var runs []models.ScanTaskRun
	if err := s.db.
		Where("status IN ?", []string{runStatusPending, runStatusRunning}).
		Order("created_at ASC").
		Find(&runs).Error; err != nil {
		return err
	}

	for _, run := range runs {
		if run.Status == runStatusRunning {
			update := map[string]interface{}{
				"status":           runStatusPending,
				"progress_message": "检测到未完成运行，已重新排队",
				"updated_at":       time.Now(),
			}
			if err := s.db.Model(&models.ScanTaskRun{}).Where("id = ?", run.ID).Updates(update).Error; err != nil {
				s.log.Warn("重置运行状态失败", "run_id", run.ID, "error", err)
				continue
			}
		}
		s.enqueueRun(run.ID)
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

	// 创建任务处理器
	handler := func(ctx context.Context, id string) error {
		if err := s.triggerScheduledTask(task.ID); err != nil {
			s.log.Error("定时任务触发失败", "task_id", task.ID, "error", err)
			return err
		}
		return nil
	}

	// 调度任务（如果已存在会自动更新）
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

func (s *ScanTaskService) enqueueRun(runID uint) {
	// 如果配置了任务队列，使用任务队列
	if s.taskQueue != nil {
		// 获取 run 信息以提取 tenantID 和 taskID
		var run models.ScanTaskRun
		if err := s.db.First(&run, runID).Error; err != nil {
			s.log.Error("获取运行信息失败", "run_id", runID, "error", err)
			return
		}

		ctx := context.Background()
		taskID := uint(0)
		if run.TaskID != nil {
			taskID = *run.TaskID
		}

		if err := s.taskQueue.EnqueueScanTask(ctx, runID, taskID, run.TenantID); err != nil {
			s.log.Error("任务入队失败", "run_id", runID, "error", err)
			// 回退：标记运行失败
			s.db.Model(&models.ScanTaskRun{}).
				Where("id = ?", runID).
				Updates(map[string]interface{}{
					"status":           runStatusFailed,
					"progress_message": fmt.Sprintf("任务入队失败: %v", err),
					"updated_at":       time.Now(),
				})
		}
		return
	}

	// 否则使用本地队列
	select {
	case s.queue <- runID:
	default:
		// 队列满时阻塞，保证任务不会丢失
		s.queue <- runID
	}
}

// CreateManualRun 创建手动扫描运行并入队
func (s *ScanTaskService) CreateManualRun(ctx context.Context, tenantID, userID uint, token string, req *models.ScanRequest) (*models.ScanTaskRun, error) {
	if req == nil {
		return nil, errors.New("请求不能为空")
	}

	if req.EngineID == 0 {
		return nil, errors.New("resource_id 不能为空")
	}

	// 尝试验证资源可访问性（主要用于快速失败）
	resource, err := s.resourceService.GetResourceByID(req.EngineID, tenantID, token)
	if err != nil {
		return nil, fmt.Errorf("验证资源失败: %w", err)
	}

	// Redis 去重检查（手动触发）
	if s.dedupService != nil {
		taskKey := s.dedupService.GenerateTaskKey(tenantID, req.EngineID, models.TriggerTypeManual)
		if s.dedupService.CheckTaskExists(ctx, taskKey) {
			return nil, fmt.Errorf("该资源正在扫描中，请稍后再试")
		}
		// 标记任务运行中（2小时超时）
		if err := s.dedupService.MarkTaskRunning(ctx, taskKey, 2*time.Hour); err != nil {
			s.log.Warn("标记任务运行失败", "error", err)
		}
	}

	params := models.JSONMap{
		"schema_names": req.SchemaNames,
		"object_paths": req.ObjectPaths,
		"scan_depth":   req.ScanDepth,
	}
	if token != "" {
		params["token"] = token
	}

	run := &models.ScanTaskRun{
		TenantID:        tenantID,
		EngineID:      req.EngineID,
		StorageType:     normalizeStorageType(resource.EngineType),
		TriggerType:     triggerTypeManual,
		Status:          runStatusPending,
		Parameters:      params,
		ProgressMessage: "已创建，等待执行",
		TriggerUserID:   uintPtr(userID),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := s.db.Create(run).Error; err != nil {
		return nil, err
	}
	setRunName(s.db, run, run.StorageType, triggerTypeManual, s.log)

	s.enqueueRun(run.ID)
	return run, nil
}

// ExecuteScanRun 执行扫描运行（供 Worker 调用）
func (s *ScanTaskService) ExecuteScanRun(ctx context.Context, runID uint) error {
	return s.executeRun(ctx, runID)
}

func (s *ScanTaskService) executeRun(ctx context.Context, runID uint) error {
	var run models.ScanTaskRun
	if err := s.db.First(&run, runID).Error; err != nil {
		return err
	}

	// 任务完成后清理去重标记和更新扫描时间
	defer func() {
		if s.dedupService != nil {
			taskKey := s.dedupService.GenerateTaskKey(run.TenantID, run.EngineID, run.TriggerType)
			if err := s.dedupService.ClearTask(context.Background(), taskKey); err != nil {
				s.log.Warn("清除任务标记失败", "run_id", runID, "error", err)
			}

			// 更新最后扫描时间
			if err := s.dedupService.UpdateLastScanTime(context.Background(), run.EngineID); err != nil {
				s.log.Warn("更新最后扫描时间失败", "run_id", runID, "error", err)
			}
		}
	}()

	if strings.TrimSpace(run.StorageType) == "" {
		storageType := s.lookupStorageType(run.EngineID, run.TenantID)
		update := map[string]interface{}{
			"storage_type": storageType,
		}
		if err := s.db.Model(&models.ScanTaskRun{}).Where("id = ?", runID).Updates(update).Error; err != nil {
			s.log.Warn("补齐运行存储类型失败", "run_id", runID, "error", err)
		} else {
			run.StorageType = storageType
			setRunName(s.db, &run, storageType, run.TriggerType, s.log)
		}
	}

	if run.Status != runStatusPending {
		s.log.Info("跳过非待执行任务", "run_id", runID, "status", run.Status)
		return nil
	}

	start := time.Now()
	if err := s.db.Model(&models.ScanTaskRun{}).
		Where("id = ? AND status = ?", runID, runStatusPending).
		Updates(map[string]interface{}{
			"status":           runStatusRunning,
			"started_at":       start,
			"progress_message": "任务开始执行",
			"updated_at":       time.Now(),
		}).Error; err != nil {
		return err
	}

	reporter := newRunProgressReporter(s, runID)
	reporter.Message("任务开始执行")

	var params struct {
		SchemaNames []string `json:"schema_names"`
		ObjectPaths []string `json:"object_paths"`
		ScanDepth   string   `json:"scan_depth"`
		Token       string   `json:"token"`
	}
	if run.Parameters != nil {
		raw, err := json.Marshal(run.Parameters)
		if err == nil {
			_ = json.Unmarshal(raw, &params)
		}
	}

	// 使用params中的scan_depth，如果未设置则默认为deep
	scanDepth := params.ScanDepth
	if scanDepth == "" {
		scanDepth = "deep"
	}

	resp, err := s.scanService.ScanResourceWithDepth(run.EngineID, run.TenantID, params.SchemaNames, params.ObjectPaths, params.Token, scanDepth, reporter)
	completeTime := time.Now()

	if err != nil {
		update := map[string]interface{}{
			"status":           runStatusFailed,
			"error_message":    err.Error(),
			"progress_message": fmt.Sprintf("执行失败: %v", err),
			"completed_at":     completeTime,
			"updated_at":       time.Now(),
		}
		if dbErr := s.db.Model(&models.ScanTaskRun{}).Where("id = ?", runID).Updates(update).Error; dbErr != nil {
			s.log.Error("更新运行失败状态出错", "run_id", runID, "error", dbErr)
		}
		return err
	}

	result := models.JSONMap{
		"schemas_scanned": resp.SchemasScanned,
		"tables_scanned":  resp.TablesScanned,
		"fields_scanned":  resp.FieldsScanned,
		"duration_ms":     resp.DurationMs,
		"started_at":      resp.StartedAt,
	}

	update := map[string]interface{}{
		"status":           runStatusSuccess,
		"result_summary":   result,
		"progress_message": "执行完成",
		"completed_at":     completeTime,
		"progress_percent": 100.0,
		"updated_at":       time.Now(),
	}
	if err := s.db.Model(&models.ScanTaskRun{}).Where("id = ?", runID).Updates(update).Error; err != nil {
		s.log.Error("更新运行成功状态出错", "run_id", runID, "error", err)
	}

	// 更新关联任务的运行时间
	if run.TaskID != nil {
		next := s.computeNextRunTime(run.TaskID)
		taskUpdate := map[string]interface{}{
			"last_run_at": completeTime,
			"updated_at":  time.Now(),
		}
		if next != nil {
			taskUpdate["next_run_at"] = *next
		}
		if err := s.db.Model(&models.ScanTask{}).Where("id = ?", *run.TaskID).Updates(taskUpdate).Error; err != nil {
			s.log.Warn("更新任务运行时间失败", "task_id", *run.TaskID, "error", err)
		}
	}

	return nil
}

func (s *ScanTaskService) updateRunProgress(runID uint, fields map[string]interface{}) {
	fields["updated_at"] = time.Now()
	if err := s.db.Model(&models.ScanTaskRun{}).Where("id = ?", runID).Updates(fields).Error; err != nil {
		s.log.Warn("更新运行进度失败", "run_id", runID, "error", err)
	}
}

// GetRun 获取运行详情
func (s *ScanTaskService) GetRun(runID uint) (*models.ScanTaskRun, error) {
	var run models.ScanTaskRun
	if err := s.db.First(&run, runID).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

// ListRuns 按租户列出任务运行
func (s *ScanTaskService) ListRuns(tenantID uint, opts *ListRunsOptions) ([]models.ScanTaskRunView, int64, error) {
	if opts == nil {
		opts = &ListRunsOptions{}
	}

	limit := opts.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	query := s.db.Table("scan_task_runs AS r").Where("r.tenant_id = ?", tenantID)
	if opts.TaskID != nil && *opts.TaskID > 0 {
		query = query.Where("r.task_id = ?", *opts.TaskID)
	}
	if opts.EngineID != nil && *opts.EngineID > 0 {
		query = query.Where("r.resource_id = ?", *opts.EngineID)
	}
	if opts.Status != "" {
		query = query.Where("r.status = ?", opts.Status)
	}
	if opts.TriggerType != "" {
		query = query.Where("r.trigger_type = ?", opts.TriggerType)
	}
	if opts.StorageType != "" {
		query = query.Where("r.storage_type = ?", opts.StorageType)
	}
	if opts.StartedAfter != nil {
		query = query.Where("r.started_at >= ?", opts.StartedAfter)
	}
	if opts.StartedBefore != nil {
		query = query.Where("r.started_at <= ?", opts.StartedBefore)
	}

	countQuery := query.Session(&gorm.Session{})
	var total int64
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	type runRecord struct {
		models.ScanTaskRun
		TaskPlanName string `gorm:"column:task_plan_name"`
	}

	var records []runRecord
	findQuery := query.Session(&gorm.Session{})
	if err := findQuery.
		Select("r.*, t.name AS task_plan_name").
		Joins("LEFT JOIN scan_tasks t ON r.task_id = t.id").
		Order("r.created_at DESC").
		Limit(limit).
		Offset(offset).
		Scan(&records).Error; err != nil {
		return nil, 0, err
	}

	if len(records) == 0 {
		return []models.ScanTaskRunView{}, total, nil
	}

	resourceCache := make(map[uint]*commonModels.Engine)
	views := make([]models.ScanTaskRunView, 0, len(records))

	for _, record := range records {
		run := record.ScanTaskRun
		res := s.ensureResourceCached(resourceCache, run.EngineID, run.TenantID)

		// 若存储类型缺失，则尝试补齐
		if strings.TrimSpace(run.StorageType) == "" && res != nil {
			run.StorageType = normalizeStorageType(res.EngineType)
			if err := s.db.Model(&models.ScanTaskRun{}).
				Where("id = ?", run.ID).
				Updates(map[string]any{
					"storage_type": run.StorageType,
					"updated_at":   time.Now(),
				}).Error; err != nil {
				s.log.Warn("补齐运行存储类型写回失败", "run_id", run.ID, "error", err)
			}
		}

		view := models.ScanTaskRunView{
			ScanTaskRun:  run,
			TaskPlanName: record.TaskPlanName,
		}

		displayName := strings.TrimSpace(run.Name)
		if displayName == "" {
			displayName = strings.TrimSpace(record.TaskPlanName)
		}
		if displayName == "" && run.TaskID != nil {
			displayName = fmt.Sprintf("任务 #%d", *run.TaskID)
		}
		if displayName == "" {
			displayName = fmt.Sprintf("运行 #%d", run.ID)
		}
		view.TaskName = displayName

		if res != nil {
			view.ResourceName = res.Name
			view.ResourceType = res.EngineType
			// 再次兜底存储类型
			if strings.TrimSpace(view.StorageType) == "" {
				view.StorageType = normalizeStorageType(res.EngineType)
			}
		}

		views = append(views, view)
	}

	return views, total, nil
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
		return nil, errors.New("resource_id 不能为空")
	}
	if req.Name == "" {
		return nil, errors.New("任务名称不能为空")
	}

	params := models.JSONMap{
		"schema_names": req.SchemaNames,
		"object_paths": req.ObjectPaths,
		"scan_depth":   req.ScanDepth,
	}

	cronExpr, scheduleConfig, err := s.buildCronExpression(req)
	if err != nil {
		return nil, err
	}

	task := &models.ScanTask{
		TenantID:       tenantID,
		EngineID:     req.EngineID,
		Name:           req.Name,
		Description:    req.Description,
		ScheduleType:   req.ScheduleType,
		Schedule:       cronExpr,
		Enabled:        req.Enabled,
		Parameters:     params,
		ScheduleConfig: scheduleConfig,
		CreatedBy:      userID,
		UpdatedBy:      userID,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if cronExpr != "" {
		if next := s.nextTimeFromSpec(cronExpr, time.Now()); next != nil {
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
		"schema_names": req.SchemaNames,
		"object_paths": req.ObjectPaths,
		"scan_depth":   req.ScanDepth,
	}

	cronExpr, scheduleConfig, err := s.buildCronExpression(req)
	if err != nil {
		return nil, err
	}

	task.Name = req.Name
	task.Description = req.Description
	task.EngineID = req.EngineID
	task.ScheduleType = req.ScheduleType
	task.Schedule = cronExpr
	task.Enabled = req.Enabled
	task.Parameters = params
	task.ScheduleConfig = scheduleConfig
	task.UpdatedBy = userID
	task.UpdatedAt = time.Now()

	if cronExpr != "" {
		next := s.nextTimeFromSpec(cronExpr, time.Now())
		task.NextRunAt = next
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
func (s *ScanTaskService) TriggerTaskNow(ctx context.Context, tenantID, taskID, userID uint) (*models.ScanTaskRun, error) {
	task, err := s.GetTask(tenantID, taskID)
	if err != nil {
		return nil, err
	}

	storageType := s.lookupStorageType(task.EngineID, task.TenantID)

	run := &models.ScanTaskRun{
		TaskID:          uintPtr(task.ID),
		TenantID:        task.TenantID,
		EngineID:      task.EngineID,
		StorageType:     storageType,
		TriggerType:     triggerTypeManual,
		Status:          runStatusPending,
		Parameters:      cloneJSONMap(task.Parameters),
		ProgressMessage: "手动触发，等待执行",
		TriggerUserID:   uintPtr(userID),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := s.db.Create(run).Error; err != nil {
		return nil, err
	}
	setRunName(s.db, run, storageType, triggerTypeManual, s.log)

	s.enqueueRun(run.ID)
	return run, nil
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

	// Redis 去重检查（定时触发）
	if s.dedupService != nil {
		taskKey := s.dedupService.GenerateTaskKey(task.TenantID, task.EngineID, models.TriggerTypeScheduled)
		if s.dedupService.CheckTaskExists(ctx, taskKey) {
			s.log.Info("该资源正在扫描中，跳过本次定时触发", "task_id", taskID)
			return nil
		}

		// 时间戳检查：定时扫描至少间隔 6 小时
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

		// 标记任务运行中（2小时超时）
		if err := s.dedupService.MarkTaskRunning(ctx, taskKey, 2*time.Hour); err != nil {
			s.log.Warn("标记任务运行失败", "error", err)
		}
	}

	storageType := s.lookupStorageType(task.EngineID, task.TenantID)

	run := &models.ScanTaskRun{
		TaskID:          uintPtr(taskID),
		TenantID:        task.TenantID,
		EngineID:      task.EngineID,
		StorageType:     storageType,
		TriggerType:     triggerTypeScheduled,
		Status:          runStatusPending,
		Parameters:      cloneJSONMap(task.Parameters),
		ProgressMessage: "定时任务等待执行",
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := s.db.Create(run).Error; err != nil {
		return err
	}
	setRunName(s.db, run, storageType, triggerTypeScheduled, s.log)

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

	s.enqueueRun(run.ID)
	return nil
}

func (s *ScanTaskService) ensureResourceCached(cache map[uint]*commonModels.Engine, engineID, tenantID uint) *commonModels.Engine {
	if cache == nil {
		return nil
	}
	if res, ok := cache[resourceID]; ok {
		return res
	}

	res, err := s.resourceService.GetResourceByID(engineID, tenantID, "")
	if err != nil {
		s.log.Warn("获取资源信息失败", "engine_id", engineID, "tenant_id", tenantID, "error", err)
		cache[resourceID] = nil
		return nil
	}

	cache[resourceID] = res
	return res
}

func (s *ScanTaskService) lookupStorageType(engineID, tenantID uint) string {
	resource, err := s.resourceService.GetResourceByID(engineID, tenantID, "")
	if err != nil {
		s.log.Warn("获取资源存储类型失败", "engine_id", engineID, "tenant_id", tenantID, "error", err)
		return "unknown"
	}
	return normalizeStorageType(resource.EngineType)
}

func (s *ScanTaskService) computeNextRunTime(taskID *uint) *time.Time {
	if taskID == nil {
		return nil
	}
	var task models.ScanTask
	if err := s.db.Select("schedule").First(&task, *taskID).Error; err != nil {
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

func (s *ScanTaskService) buildCronExpression(req *models.ScanTaskUpsertRequest) (string, models.JSONMap, error) {
	// 使用公共 ExpressionBuilder
	scheduleConfig := commonScheduler.ScheduleConfig{
		Type:  req.ScheduleType,
		Time:  req.ScheduleTime,
		Value: req.ScheduleValue,
		Expr:  req.Schedule,
	}

	cronExpr, config, err := s.exprBuilder.BuildFromScheduleConfig(scheduleConfig)
	if err != nil {
		return "", nil, err
	}

	// 转换为 models.JSONMap 类型
	jsonConfig := models.JSONMap{}
	for k, v := range config {
		jsonConfig[k] = v
	}

	return cronExpr, jsonConfig, nil
}

// runProgressReporter 实现 ScanProgressReporter，将回调写入数据库
type runProgressReporter struct {
	service *ScanTaskService
	runID   uint

	total int
	mu    sync.Mutex
	stats map[string]int64
}

func newRunProgressReporter(service *ScanTaskService, runID uint) *runProgressReporter {
	return &runProgressReporter{
		service: service,
		runID:   runID,
		stats:   make(map[string]int64),
	}
}

func (r *runProgressReporter) SetTotal(total int) {
	r.mu.Lock()
	r.total = total
	r.mu.Unlock()
	r.service.updateRunProgress(r.runID, map[string]interface{}{
		"progress_total": total,
	})
}

func (r *runProgressReporter) Advance(label string, completed, total int, meta map[string]interface{}) {
	r.mu.Lock()
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
	progress := map[string]interface{}{
		"progress_current": completed,
		"progress_total":   total,
		"progress_percent": calcProgressPercent(completed, total),
		"progress_message": fmt.Sprintf("已完成 %d/%d，最新完成: %s", completed, total, label),
		"result_summary":   cloneAnyMap(r.stats),
	}
	r.mu.Unlock()

	r.service.updateRunProgress(r.runID, progress)
}

func (r *runProgressReporter) Message(message string) {
	r.service.updateRunProgress(r.runID, map[string]interface{}{
		"progress_message": message,
	})
}

func calcProgressPercent(done, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(done) * 100.0 / float64(total)
}

func uintPtr(v uint) *uint {
	if v == 0 {
		return nil
	}
	return &v
}

func cloneAnyMap(stats map[string]int64) models.JSONMap {
	if len(stats) == 0 {
		return nil
	}
	result := models.JSONMap{}
	for k, v := range stats {
		result[k] = v
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

// CreateOrUpdateTaskFromScanConfig 根据资源的扫描配置创建或更新自动扫描任务
func (s *ScanTaskService) CreateOrUpdateTaskFromScanConfig(resource *commonModels.Engine) error {
	if resource == nil || resource.ScanConfig == nil || !resource.ScanConfig.Enabled {
		return nil
	}

	scanConfig := resource.ScanConfig

	// 处理 TenantID（可能为 nil，SuperAdmin 创建的资源）
	var tenantID uint
	if resource.TenantID != nil {
		tenantID = *resource.TenantID
	} else {
		tenantID = 0 // SuperAdmin 的资源，使用 0
	}

	// 查找是否已有该资源的自动扫描任务
	var existingTask models.ScanTask
	err := s.db.Where("resource_id = ? AND tenant_id = ? AND name LIKE ?",
		resource.ID, tenantID, "自动扫描%").First(&existingTask).Error

	// 构建任务名称
	taskName := fmt.Sprintf("自动扫描 - %s", resource.Name)

	// 构建 Cron 表达式
	cronExpr, err := s.buildCronExpressionFromScanConfig(scanConfig)
	if err != nil {
		return fmt.Errorf("构建 Cron 表达式失败: %w", err)
	}

	// 构建任务参数：定时扫描固定使用 deep 深度，不使用 schema_names 和 object_paths（系统自动过滤）
	parameters := models.JSONMap{
		"scan_depth": "deep", // 定时扫描固定深度
	}

	// 如果任务不存在，创建新任务
	if errors.Is(err, gorm.ErrRecordNotFound) {
		task := &models.ScanTask{
			TenantID:       tenantID,
			EngineID:     resource.ID,
			Name:           taskName,
			Description:    "由存储引擎注册时自动创建",
			ScheduleType:   scanConfig.ScheduleType,
			Schedule:       cronExpr,
			Enabled:        true,
			Parameters:     parameters,
		}

		if err := s.db.Create(task).Error; err != nil {
			return fmt.Errorf("创建自动扫描任务失败: %w", err)
		}

		// 调度任务
		if err := s.scheduleTask(task); err != nil {
			s.log.Warn("调度任务失败", "task_id", task.ID, "error", err)
		}

		s.log.Info("自动扫描任务已创建",
			"task_id", task.ID,
			"engine_id", resource.ID,
			"resource_name", resource.Name,
			"schedule_type", scanConfig.ScheduleType)

		return nil
	} else if err != nil {
		return fmt.Errorf("查询已有任务失败: %w", err)
	}

	// 任务已存在，更新配置
	updates := map[string]interface{}{
		"name":            taskName,
		"schedule_type":   scanConfig.ScheduleType,
		"schedule":        cronExpr,
		"enabled":         scanConfig.Enabled,
		"parameters":      parameters,
		"updated_at":      time.Now(),
	}

	if err := s.db.Model(&existingTask).Updates(updates).Error; err != nil {
		return fmt.Errorf("更新自动扫描任务失败: %w", err)
	}

	// 重新调度任务
	s.unscheduleTask(existingTask.ID)
	updatedTask := existingTask
	updatedTask.Name = taskName
	updatedTask.ScheduleType = scanConfig.ScheduleType
	updatedTask.Schedule = cronExpr
	updatedTask.Enabled = scanConfig.Enabled
	updatedTask.Parameters = parameters

	if err := s.scheduleTask(&updatedTask); err != nil {
		s.log.Warn("重新调度任务失败", "task_id", updatedTask.ID, "error", err)
	}

	s.log.Info("自动扫描任务已更新",
		"task_id", existingTask.ID,
		"engine_id", resource.ID,
		"resource_name", resource.Name,
		"schedule_type", scanConfig.ScheduleType)

	return nil
}

// DeleteTaskByResourceID 删除指定资源关联的所有自动扫描任务
func (s *ScanTaskService) DeleteTaskByResourceID(engineID uint) error {
	var tasks []models.ScanTask
	if err := s.db.Where("resource_id = ? AND name LIKE ?", engineID, "自动扫描%").Find(&tasks).Error; err != nil {
		return fmt.Errorf("查询资源关联任务失败: %w", err)
	}

	if len(tasks) == 0 {
		return nil // 没有任务需要删除
	}

	for _, task := range tasks {
		// 取消调度
		s.unscheduleTask(task.ID)

		// 删除任务
		if err := s.db.Delete(&task).Error; err != nil {
			s.log.Warn("删除任务失败", "task_id", task.ID, "error", err)
			continue
		}

		s.log.Info("自动扫描任务已删除", "task_id", task.ID, "engine_id", engineID)
	}

	return nil
}

// buildCronExpressionFromScanConfig 根据 ScanConfig 构建 Cron 表达式
func (s *ScanTaskService) buildCronExpressionFromScanConfig(config *commonModels.ScanConfig) (string, error) {
	if config == nil {
		return "", errors.New("扫描配置为空")
	}

	switch config.ScheduleType {
	case "manual":
		return "", nil // 手动触发，不需要 Cron 表达式

	case "cron":
		if config.CronExpression == "" {
			return "", errors.New("Cron 类型必须提供 cron_expression")
		}
		// 验证 Cron 表达式
		if err := s.exprBuilder.Validate(config.CronExpression); err != nil {
			return "", fmt.Errorf("无效的 Cron 表达式: %w", err)
		}
		return config.CronExpression, nil

	case "daily":
		// 每日执行：从 schedule_time 解析 HH:mm
		hour, minute := 0, 0
		if config.ScheduleTime != "" {
			parts := strings.Split(config.ScheduleTime, ":")
			if len(parts) == 2 {
				if h, err := strconv.Atoi(parts[0]); err == nil {
					hour = h
				}
				if m, err := strconv.Atoi(parts[1]); err == nil {
					minute = m
				}
			}
		}
		return fmt.Sprintf("%d %d * * *", minute, hour), nil

	case "weekly":
		// 每周执行：从 schedule_time 解析 HH:mm，从 schedule_value 获取星期几
		hour, minute := 0, 0
		if config.ScheduleTime != "" {
			parts := strings.Split(config.ScheduleTime, ":")
			if len(parts) == 2 {
				if h, err := strconv.Atoi(parts[0]); err == nil {
					hour = h
				}
				if m, err := strconv.Atoi(parts[1]); err == nil {
					minute = m
				}
			}
		}
		days := "0" // 默认周日
		if len(config.ScheduleValue) > 0 {
			dayStrs := make([]string, len(config.ScheduleValue))
			for i, day := range config.ScheduleValue {
				dayStrs[i] = strconv.Itoa(day)
			}
			days = strings.Join(dayStrs, ",")
		}
		return fmt.Sprintf("%d %d * * %s", minute, hour, days), nil

	case "monthly":
		// 每月执行：从 schedule_time 解析 HH:mm，从 schedule_value 获取日期
		hour, minute := 0, 0
		if config.ScheduleTime != "" {
			parts := strings.Split(config.ScheduleTime, ":")
			if len(parts) == 2 {
				if h, err := strconv.Atoi(parts[0]); err == nil {
					hour = h
				}
				if m, err := strconv.Atoi(parts[1]); err == nil {
					minute = m
				}
			}
		}
		dates := "1" // 默认每月1号
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
