package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/addp/common/events"
	"github.com/addp/common/logger"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// CleanupOrchestratorService 清理任务编排服务
type CleanupOrchestratorService struct {
	redis *redis.Client
	log   *slog.Logger
}

// NewCleanupOrchestratorService 创建清理编排服务
func NewCleanupOrchestratorService(redisClient *redis.Client) *CleanupOrchestratorService {
	return &CleanupOrchestratorService{
		redis: redisClient,
		log:   logger.With("component", "cleanup_orchestrator"),
	}
}

// CreateScanTask 创建扫描任务
func (s *CleanupOrchestratorService) CreateScanTask(ctx context.Context, tenantID uint, scope []string, userID uint) (string, error) {
	// 生成任务ID
	taskID := fmt.Sprintf("cleanup-scan-%d-%s", time.Now().Unix(), uuid.New().String()[:8])

	// 默认扫描所有模块
	if len(scope) == 0 {
		scope = []string{events.ModuleMeta, events.ModuleManager, events.ModuleTransfer}
	}

	// 创建任务
	task := models.CleanupTask{
		TaskID:          taskID,
		Action:          events.CleanupActionScan,
		TenantID:        tenantID,
		Status:          "pending",
		ExpectedModules: scope,
		RequestedBy:     userID,
		StartedAt:       time.Now().Format(time.RFC3339),
		TimeoutAt:       time.Now().Add(30 * time.Second).Format(time.RFC3339),
	}

	// 写入Redis
	taskJSON, err := json.Marshal(task)
	if err != nil {
		return "", fmt.Errorf("序列化任务失败: %w", err)
	}

	taskKey := fmt.Sprintf("cleanup:tasks:%s", taskID)
	err = s.redis.HSet(ctx, taskKey, "data", string(taskJSON)).Err()
	if err != nil {
		return "", fmt.Errorf("保存任务失败: %w", err)
	}

	// 设置过期时间（1小时）
	s.redis.Expire(ctx, taskKey, 1*time.Hour)

	// 发布事件到 Redis Stream
	event := events.CleanupRequestEvent{
		TaskID:          taskID,
		Action:          events.CleanupActionScan,
		TenantID:        tenantID,
		ExpectedModules: scope,
		RequestedBy:     userID,
		RequestedAt:     time.Now(),
	}

	if err := s.publishEvent(ctx, event); err != nil {
		return "", fmt.Errorf("发布事件失败: %w", err)
	}

	// 记录历史
	historyKey := fmt.Sprintf("cleanup:history:%d", tenantID)
	s.redis.LPush(ctx, historyKey, taskID)
	s.redis.LTrim(ctx, historyKey, 0, 99) // 只保留最近100条

	s.log.Info("扫描任务已创建",
		"task_id", taskID,
		"tenant_id", tenantID,
		"scope", scope,
		"user_id", userID)

	return taskID, nil
}

// CreateExecuteTask 创建清理执行任务
func (s *CleanupOrchestratorService) CreateExecuteTask(
	ctx context.Context,
	basedOnScan string,
	deleteType string,
	userID uint,
) (string, error) {
	// 验证基础扫描任务
	scanTask, err := s.GetTaskStatus(ctx, basedOnScan)
	if err != nil {
		return "", fmt.Errorf("扫描任务不存在: %w", err)
	}

	// 生成任务ID
	taskID := fmt.Sprintf("cleanup-exec-%d-%s", time.Now().Unix(), uuid.New().String()[:8])

	// 创建任务
	task := models.CleanupTask{
		TaskID:          taskID,
		Action:          events.CleanupActionExecute,
		TenantID:        scanTask.Task.TenantID,
		DeleteType:      deleteType,
		Status:          "pending",
		ExpectedModules: scanTask.Task.ExpectedModules,
		RequestedBy:     userID,
		StartedAt:       time.Now().Format(time.RFC3339),
		TimeoutAt:       time.Now().Add(5 * time.Minute).Format(time.RFC3339), // 执行任务超时5分钟
		BasedOnScan:     basedOnScan,
	}

	// 保存任务
	taskJSON, err := json.Marshal(task)
	if err != nil {
		return "", fmt.Errorf("序列化任务失败: %w", err)
	}

	taskKey := fmt.Sprintf("cleanup:tasks:%s", taskID)
	err = s.redis.HSet(ctx, taskKey, "data", string(taskJSON)).Err()
	if err != nil {
		return "", fmt.Errorf("保存任务失败: %w", err)
	}

	s.redis.Expire(ctx, taskKey, 1*time.Hour)

	// 发布事件
	event := events.CleanupRequestEvent{
		TaskID:          taskID,
		Action:          events.CleanupActionExecute,
		TenantID:        task.TenantID,
		DeleteType:      deleteType,
		ExpectedModules: task.ExpectedModules,
		BasedOnScan:     basedOnScan,
		RequestedBy:     userID,
		RequestedAt:     time.Now(),
	}

	if err := s.publishEvent(ctx, event); err != nil {
		return "", fmt.Errorf("发布事件失败: %w", err)
	}

	// 记录历史
	historyKey := fmt.Sprintf("cleanup:history:%d", task.TenantID)
	s.redis.LPush(ctx, historyKey, taskID)
	s.redis.LTrim(ctx, historyKey, 0, 99)

	s.log.Info("执行任务已创建",
		"task_id", taskID,
		"tenant_id", task.TenantID,
		"delete_type", deleteType,
		"based_on_scan", basedOnScan,
		"user_id", userID)

	return taskID, nil
}

// GetTaskStatus 查询任务状态
func (s *CleanupOrchestratorService) GetTaskStatus(ctx context.Context, taskID string) (*models.TaskStatusResponse, error) {
	// 读取任务信息
	taskKey := fmt.Sprintf("cleanup:tasks:%s", taskID)
	taskDataStr, err := s.redis.HGet(ctx, taskKey, "data").Result()
	if err == redis.Nil {
		return nil, fmt.Errorf("task not found")
	}
	if err != nil {
		return nil, err
	}

	var task models.CleanupTask
	if err := json.Unmarshal([]byte(taskDataStr), &task); err != nil {
		return nil, err
	}

	// 读取各模块结果
	resultsKey := fmt.Sprintf("cleanup:results:%s", taskID)
	resultsMap, err := s.redis.HGetAll(ctx, resultsKey).Result()
	if err != nil {
		return nil, err
	}

	// 解析结果
	results := make(map[string]interface{})
	progress := models.TaskProgress{
		Total:   len(task.ExpectedModules),
		Modules: make(map[string]string),
	}

	// 检查超时时间
	timeoutAt, _ := time.Parse(time.RFC3339, task.TimeoutAt)
	isTimeout := time.Now().After(timeoutAt)

	for _, module := range task.ExpectedModules {
		if resultStr, ok := resultsMap[module]; ok {
			var result events.CleanupResultData
			if err := json.Unmarshal([]byte(resultStr), &result); err == nil {
				results[module] = result
				progress.Modules[module] = result.Status
				if result.Status == "success" || result.Status == "failed" {
					progress.Completed++
				}
			}
		} else {
			// 检查超时
			if isTimeout {
				progress.Modules[module] = "timeout"
				progress.Completed++
			} else {
				progress.Modules[module] = "pending"
			}
		}
	}

	// 计算整体状态
	overallStatus := s.calculateOverallStatus(&task, &progress, isTimeout)

	// 汇总统计
	var summary interface{}
	if task.Action == events.CleanupActionScan {
		summary = s.aggregateScanSummary(results)
	} else {
		summary = s.aggregateExecuteSummary(results)
	}

	return &models.TaskStatusResponse{
		TaskID:   taskID,
		Action:   task.Action,
		Status:   overallStatus,
		Progress: progress,
		Results:  results,
		Summary:  summary,
		Task:     task,
	}, nil
}

// GetTaskHistory 获取历史任务列表
func (s *CleanupOrchestratorService) GetTaskHistory(ctx context.Context, tenantID uint, limit int) ([]models.CleanupTask, error) {
	if limit <= 0 {
		limit = 20
	}

	historyKey := fmt.Sprintf("cleanup:history:%d", tenantID)
	taskIDs, err := s.redis.LRange(ctx, historyKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}

	tasks := make([]models.CleanupTask, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		taskKey := fmt.Sprintf("cleanup:tasks:%s", taskID)
		taskDataStr, err := s.redis.HGet(ctx, taskKey, "data").Result()
		if err != nil {
			continue // 任务可能已过期
		}

		var task models.CleanupTask
		if err := json.Unmarshal([]byte(taskDataStr), &task); err != nil {
			continue
		}

		tasks = append(tasks, task)
	}

	return tasks, nil
}

// publishEvent 发布事件到 Redis Stream
func (s *CleanupOrchestratorService) publishEvent(ctx context.Context, event events.CleanupRequestEvent) error {
	// 将 expected_modules 序列化为 JSON
	modulesJSON, err := json.Marshal(event.ExpectedModules)
	if err != nil {
		return fmt.Errorf("序列化 expected_modules 失败: %w", err)
	}

	// 将事件转换为 map，注意 Redis Stream 不支持嵌套结构，所以需要序列化复杂字段
	eventMap := map[string]interface{}{
		"task_id":           event.TaskID,
		"action":            event.Action,
		"tenant_id":         event.TenantID,
		"delete_type":       event.DeleteType,
		"expected_modules":  string(modulesJSON),
		"based_on_scan":     event.BasedOnScan,
		"requested_by":      event.RequestedBy,
		"requested_at":      event.RequestedAt.Format(time.RFC3339),
	}

	// 发布到 Redis Stream
	_, err = s.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: events.EventCleanupRequest,
		Values: eventMap,
	}).Result()

	return err
}

// calculateOverallStatus 计算整体状态
func (s *CleanupOrchestratorService) calculateOverallStatus(
	task *models.CleanupTask,
	progress *models.TaskProgress,
	isTimeout bool,
) string {
	if progress.Completed == progress.Total {
		// 全部完成
		hasFailure := false
		for _, status := range progress.Modules {
			if status == "failed" || status == "timeout" {
				hasFailure = true
				break
			}
		}
		if hasFailure {
			return "completed_with_errors"
		}
		return "completed"
	}

	if isTimeout {
		return "timeout"
	}

	if progress.Completed > 0 {
		return "running"
	}

	return "pending"
}

// aggregateScanSummary 汇总扫描统计
func (s *CleanupOrchestratorService) aggregateScanSummary(results map[string]interface{}) models.TaskSummary {
	summary := models.TaskSummary{
		RiskLevel: "low",
	}

	totalItems := 0

	for _, result := range results {
		if resultData, ok := result.(events.CleanupResultData); ok {
			if resultData.Status != "success" {
				continue
			}

			// 尝试从 statistics 中累加数据
			stats := resultData.Statistics
			// Meta 模块统计
			if invalidEngines, ok := stats["invalid_engines"].(map[string]interface{}); ok {
				if count, ok := invalidEngines["count"].(float64); ok {
					totalItems += int(count)
				}
			}
			if orphanItems, ok := stats["orphan_items"].(map[string]interface{}); ok {
				if count, ok := orphanItems["count"].(float64); ok {
					totalItems += int(count)
				}
			}
			if softDeleted, ok := stats["soft_deleted"].(map[string]interface{}); ok {
				if nodes, ok := softDeleted["nodes"].(float64); ok {
					totalItems += int(nodes)
				}
				if items, ok := softDeleted["items"].(float64); ok {
					totalItems += int(items)
				}
			}

			// Manager 模块统计
			if orphanVectors, ok := stats["orphan_vectors"].(map[string]interface{}); ok {
				if count, ok := orphanVectors["count"].(float64); ok {
					totalItems += int(count)
				}
			}
			if orphanMVT, ok := stats["orphan_mvt_tiles"].(map[string]interface{}); ok {
				if count, ok := orphanMVT["count"].(float64); ok {
					totalItems += int(count)
				}
				if sizeMB, ok := orphanMVT["size_mb"].(float64); ok {
					summary.TotalSizeMB += sizeMB
				}
			}
		}
	}

	summary.TotalItemsToClean = totalItems

	// 评估风险等级
	if totalItems > 1000 {
		summary.RiskLevel = "high"
	} else if totalItems > 100 {
		summary.RiskLevel = "medium"
	} else {
		summary.RiskLevel = "low"
	}

	return summary
}

// aggregateExecuteSummary 汇总执行统计
func (s *CleanupOrchestratorService) aggregateExecuteSummary(results map[string]interface{}) models.ExecuteSummary {
	summary := models.ExecuteSummary{}

	for _, result := range results {
		if resultData, ok := result.(events.CleanupResultData); ok {
			if resultData.Error != "" {
				summary.HasErrors = true
			}

			stats := resultData.Statistics
			// Meta 模块执行结果
			if deletedNodes, ok := stats["deleted_nodes"].(float64); ok {
				summary.TotalDeleted += int(deletedNodes)
			}
			if deletedItems, ok := stats["deleted_items"].(float64); ok {
				summary.TotalDeleted += int(deletedItems)
			}

			// Manager 模块执行结果
			if deletedVectors, ok := stats["deleted_vectors"].(float64); ok {
				summary.TotalDeleted += int(deletedVectors)
			}
			if deletedMVT, ok := stats["deleted_mvt_tiles"].(float64); ok {
				summary.TotalDeleted += int(deletedMVT)
			}
			if freedSpace, ok := stats["freed_space_mb"].(float64); ok {
				summary.FreedSpaceMB += freedSpace
			}
		}
	}

	return summary
}
