package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// ScanTaskService 管理扫描任务、运行队列与调度
type ScanTaskService struct {
	db              *gorm.DB
	scanService     *ScanServiceNew
	resourceService *ResourceService
	log             *slog.Logger

	queue        chan uint
	workers      int
	stopCh       chan struct{}
	wg           sync.WaitGroup
	cron         *cron.Cron
	parser       cron.Parser
	taskEntryIDs map[uint]cron.EntryID
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
func NewScanTaskService(db *gorm.DB, scanService *ScanServiceNew, resourceService *ResourceService) *ScanTaskService {
	if scanService == nil {
		scanService = NewScanServiceNew(db, resourceService)
	}

	return &ScanTaskService{
		db:              db,
		scanService:     scanService,
		resourceService: resourceService,
		log:             logger.With("component", "scan_task_service"),
		queue:           make(chan uint, 128),
		workers:         2,
		stopCh:          make(chan struct{}),
		cron:            cron.New(),
		parser:          cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
		taskEntryIDs:    make(map[uint]cron.EntryID),
	}
}

// Start 启动任务服务（队列消费者 + 定时调度）
func (s *ScanTaskService) Start(ctx context.Context) error {
	s.log.Info("启动扫描任务服务")

	// 启动工作协程
	for i := 0; i < s.workers; i++ {
		s.wg.Add(1)
		go s.workerLoop()
	}

	// 恢复未完成的运行
	if err := s.recoverPendingRuns(); err != nil {
		s.log.Warn("恢复历史运行失败", "error", err)
	}

	// 加载并调度定时任务
	if err := s.bootstrapSchedules(); err != nil {
		s.log.Warn("加载定时任务失败", "error", err)
	}

	s.cron.Start()
	return nil
}

// Stop 停止任务服务
func (s *ScanTaskService) Stop(ctx context.Context) {
	s.log.Info("停止扫描任务服务")
	close(s.stopCh)
	s.cron.Stop()
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
		if task.CronExpression == "" {
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
	if task.CronExpression == "" || !task.Enabled {
		return nil
	}

	s.cronMu.Lock()
	defer s.cronMu.Unlock()

	// 如果已有旧的 Entry，先移除
	if entryID, ok := s.taskEntryIDs[task.ID]; ok {
		s.cron.Remove(entryID)
		delete(s.taskEntryIDs, task.ID)
	}

	entryID, err := s.cron.AddFunc(task.CronExpression, func() {
		if err := s.triggerScheduledTask(task.ID); err != nil {
			s.log.Error("定时任务触发失败", "task_id", task.ID, "error", err)
		}
	})
	if err != nil {
		return err
	}
	s.taskEntryIDs[task.ID] = entryID
	return nil
}

func (s *ScanTaskService) unscheduleTask(taskID uint) {
	s.cronMu.Lock()
	defer s.cronMu.Unlock()
	if entryID, ok := s.taskEntryIDs[taskID]; ok {
		s.cron.Remove(entryID)
		delete(s.taskEntryIDs, taskID)
	}
}

func (s *ScanTaskService) enqueueRun(runID uint) {
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

	if req.ResourceID == 0 {
		return nil, errors.New("resource_id 不能为空")
	}

	// 尝试验证资源可访问性（主要用于快速失败）
	resource, err := s.resourceService.GetResourceByID(req.ResourceID, tenantID, token)
	if err != nil {
		return nil, fmt.Errorf("验证资源失败: %w", err)
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
		ResourceID:      req.ResourceID,
		StorageType:     normalizeStorageType(resource.ResourceType),
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

func (s *ScanTaskService) executeRun(ctx context.Context, runID uint) error {
	var run models.ScanTaskRun
	if err := s.db.First(&run, runID).Error; err != nil {
		return err
	}

	if strings.TrimSpace(run.StorageType) == "" {
		storageType := s.lookupStorageType(run.ResourceID, run.TenantID)
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

	resp, err := s.scanService.ScanResourceWithDepth(run.ResourceID, run.TenantID, params.SchemaNames, params.ObjectPaths, params.Token, scanDepth, reporter)
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
	if opts.ResourceID != nil && *opts.ResourceID > 0 {
		query = query.Where("r.resource_id = ?", *opts.ResourceID)
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

	resourceCache := make(map[uint]*commonModels.Resource)
	views := make([]models.ScanTaskRunView, 0, len(records))

	for _, record := range records {
		run := record.ScanTaskRun
		res := s.ensureResourceCached(resourceCache, run.ResourceID, run.TenantID)

		// 若存储类型缺失，则尝试补齐
		if strings.TrimSpace(run.StorageType) == "" && res != nil {
			run.StorageType = normalizeStorageType(res.ResourceType)
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
			view.ResourceType = res.ResourceType
			// 再次兜底存储类型
			if strings.TrimSpace(view.StorageType) == "" {
				view.StorageType = normalizeStorageType(res.ResourceType)
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
	if req.ResourceID == 0 {
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
		ResourceID:     req.ResourceID,
		Name:           req.Name,
		Description:    req.Description,
		ScheduleType:   req.ScheduleType,
		CronExpression: cronExpr,
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

	if task.CronExpression != "" && task.Enabled {
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
	task.ResourceID = req.ResourceID
	task.ScheduleType = req.ScheduleType
	task.CronExpression = cronExpr
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

	if task.CronExpression != "" && task.Enabled {
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

	storageType := s.lookupStorageType(task.ResourceID, task.TenantID)

	run := &models.ScanTaskRun{
		TaskID:          uintPtr(task.ID),
		TenantID:        task.TenantID,
		ResourceID:      task.ResourceID,
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
	var task models.ScanTask
	if err := s.db.First(&task, taskID).Error; err != nil {
		return err
	}
	if !task.Enabled {
		return nil
	}

	storageType := s.lookupStorageType(task.ResourceID, task.TenantID)

	run := &models.ScanTaskRun{
		TaskID:          uintPtr(taskID),
		TenantID:        task.TenantID,
		ResourceID:      task.ResourceID,
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
	next := s.nextTimeFromSpec(task.CronExpression, now)
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

func (s *ScanTaskService) ensureResourceCached(cache map[uint]*commonModels.Resource, resourceID, tenantID uint) *commonModels.Resource {
	if cache == nil {
		return nil
	}
	if res, ok := cache[resourceID]; ok {
		return res
	}

	res, err := s.resourceService.GetResourceByID(resourceID, tenantID, "")
	if err != nil {
		s.log.Warn("获取资源信息失败", "resource_id", resourceID, "tenant_id", tenantID, "error", err)
		cache[resourceID] = nil
		return nil
	}

	cache[resourceID] = res
	return res
}

func (s *ScanTaskService) lookupStorageType(resourceID, tenantID uint) string {
	resource, err := s.resourceService.GetResourceByID(resourceID, tenantID, "")
	if err != nil {
		s.log.Warn("获取资源存储类型失败", "resource_id", resourceID, "tenant_id", tenantID, "error", err)
		return "unknown"
	}
	return normalizeStorageType(resource.ResourceType)
}

func (s *ScanTaskService) computeNextRunTime(taskID *uint) *time.Time {
	if taskID == nil {
		return nil
	}
	var task models.ScanTask
	if err := s.db.Select("cron_expression").First(&task, *taskID).Error; err != nil {
		return nil
	}
	return s.nextTimeFromSpec(task.CronExpression, time.Now())
}

func (s *ScanTaskService) nextTimeFromSpec(spec string, from time.Time) *time.Time {
	if spec == "" {
		return nil
	}
	sched, err := s.parser.Parse(spec)
	if err != nil {
		s.log.Warn("解析 Cron 表达式失败", "spec", spec, "error", err)
		return nil
	}
	next := sched.Next(from)
	return &next
}

func (s *ScanTaskService) buildCronExpression(req *models.ScanTaskUpsertRequest) (string, models.JSONMap, error) {
	config := models.JSONMap{
		"type":  req.ScheduleType,
		"time":  req.ScheduleTime,
		"value": req.ScheduleValue,
	}

	switch strings.ToLower(req.ScheduleType) {
	case "manual":
		return "", config, nil
	case "daily":
		hour, minute, err := parseTimeOfDay(req.ScheduleTime)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("%d %d * * *", minute, hour), config, nil
	case "weekly":
		hour, minute, err := parseTimeOfDay(req.ScheduleTime)
		if err != nil {
			return "", nil, err
		}
		field, err := formatCronList(req.ScheduleValue, 0, 6, []int{0})
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("%d %d * * %s", minute, hour, field), config, nil
	case "monthly":
		hour, minute, err := parseTimeOfDay(req.ScheduleTime)
		if err != nil {
			return "", nil, err
		}
		field, err := formatCronList(req.ScheduleValue, 1, 31, []int{1})
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("%d %d %s * *", minute, hour, field), config, nil
	case "cron":
		if req.CronExpression == "" {
			return "", nil, errors.New("cron_expression 不能为空")
		}
		config["cron_expression"] = req.CronExpression
		return req.CronExpression, config, nil
	default:
		return "", nil, fmt.Errorf("不支持的 schedule_type: %s", req.ScheduleType)
	}
}

func parseTimeOfDay(value string) (int, int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, 0, nil
	}
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("时间格式非法，应为 HH:MM，当前为: %s", value)
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("小时格式非法: %s", parts[0])
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("分钟格式非法: %s", parts[1])
	}
	return hour, minute, nil
}

func formatCronList(values []int, min, max int, defaults []int) (string, error) {
	if len(values) == 0 {
		values = defaults
	}

	valid := make([]int, 0, len(values))
	seen := make(map[int]bool)
	for _, v := range values {
		if v < min || v > max {
			return "", fmt.Errorf("数值 %d 超出范围 [%d-%d]", v, min, max)
		}
		if !seen[v] {
			valid = append(valid, v)
			seen[v] = true
		}
	}

	if len(valid) == 0 {
		valid = defaults
	}

	sort.Ints(valid)

	parts := make([]string, len(valid))
	for i, v := range valid {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ","), nil
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
