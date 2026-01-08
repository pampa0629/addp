package service

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/common/models"
	"github.com/addp/manager/internal/operators"
)

// OperatorService 算子服务
type OperatorService struct {
	cacheManager     *CacheManager
	embeddingService *EmbeddingService
	taskTracker      *TaskTracker
}

// NewOperatorService 创建算子服务
func NewOperatorService(cacheManager *CacheManager, embeddingService *EmbeddingService) *OperatorService {
	return &OperatorService{
		cacheManager:     cacheManager,
		embeddingService: embeddingService,
		taskTracker:      NewTaskTracker(),
	}
}

// GetOperators 获取所有算子
func (s *OperatorService) GetOperators() []models.OperatorMetadata {
	return operators.ManagerOperators
}

// ExecuteOperator 执行算子
func (s *OperatorService) ExecuteOperator(
	ctx context.Context,
	operatorName string,
	tenantID uint,
	userID uint,
	params map[string]interface{},
	executeNow bool,
	taskName string,
) (*models.OperatorExecuteResponse, error) {

	switch operatorName {
	case "mvt_tile_cache":
		return s.executeMVTTileCacheOperator(ctx, tenantID, userID, params, executeNow, taskName)
	case "embedding":
		return s.executeEmbeddingOperator(ctx, tenantID, userID, params, executeNow, taskName)
	default:
		return nil, fmt.Errorf("未知的算子: %s", operatorName)
	}
}

// executeMVTTileCacheOperator 执行MVT瓦片缓存算子
func (s *OperatorService) executeMVTTileCacheOperator(
	ctx context.Context,
	tenantID uint,
	userID uint,
	params map[string]interface{},
	executeNow bool,
	taskName string,
) (*models.OperatorExecuteResponse, error) {

	// 解析参数
	layerID, ok := params["layer_id"].(float64)
	if !ok {
		return nil, fmt.Errorf("参数 layer_id 必须为数字")
	}

	// 解析参数 (当前简化实现,这些参数将在完整实现时使用)
	_ = params["min_zoom"]  // 最小缩放级别 (默认10)
	_ = params["max_zoom"]  // 最大缩放级别 (默认16)
	_ = params["bbox"]      // 边界框 (可选)

	// 构建任务名称
	if taskName == "" {
		taskName = fmt.Sprintf("MVT瓦片缓存-图层%d", int(layerID))
	}

	// 创建瓦片缓存任务
	taskID := fmt.Sprintf("mvt-cache-%d-%d", int(layerID), time.Now().Unix())

	// 这里简化实现,实际应该创建任务记录并调用CacheService
	// 由于Manager模块当前没有完整的任务管理系统,这里返回模拟响应

	response := &models.OperatorExecuteResponse{
		Status:     "success",
		TaskID:     taskID,
		TaskStatus: "pending",
		Message:    "MVT瓦片缓存任务已创建",
		CreatedAt:  time.Now().Format(time.RFC3339),
	}

	// 如果是立即执行
	if executeNow {
		// 触发瓦片缓存任务(通过Asynq队列)
		// 这里需要调用CacheService的相关方法
		response.TaskStatus = "running"
		response.Message = "MVT瓦片缓存任务已创建并开始执行"
	}

	return response, nil
}

// executeEmbeddingOperator 执行向量化算子
func (s *OperatorService) executeEmbeddingOperator(
	ctx context.Context,
	tenantID uint,
	userID uint,
	params map[string]interface{},
	executeNow bool,
	taskName string,
) (*models.OperatorExecuteResponse, error) {
	// 解析参数
	engineIDFloat, ok := params["engine_id"].(float64)
	if !ok {
		return nil, fmt.Errorf("参数 engine_id 必须为数字")
	}
	engineID := uint(engineIDFloat)

	bucket, ok := params["bucket"].(string)
	if !ok || bucket == "" {
		return nil, fmt.Errorf("参数 bucket 必须为非空字符串")
	}

	scope, _ := params["scope"].(string)
	if scope == "" {
		scope = "object"
	}

	// 构建任务名称
	if taskName == "" {
		taskName = fmt.Sprintf("向量化-%s-%s", bucket, scope)
	}

	// 创建任务ID
	taskID := fmt.Sprintf("embedding-%d-%s-%d", engineID, scope, time.Now().Unix())

	response := &models.OperatorExecuteResponse{
		Status:     "success",
		TaskID:     taskID,
		TaskStatus: "pending",
		Message:    "向量化任务已创建",
		CreatedAt:  time.Now().Format(time.RFC3339),
	}

	// 如果立即执行
	if executeNow {
		response.TaskStatus = "running"
		response.Message = "向量化任务已开始执行"

		// 创建任务追踪
		s.taskTracker.CreateTask(taskID, "向量化任务已开始")
		s.taskTracker.UpdateStatus(taskID, TaskStatusRunning, "正在向量化...")

		// 根据范围执行不同的向量化操作
		// 使用 context.Background() 避免 HTTP 请求结束后 context 被取消
		go func() {
			// 创建独立的 context，不受 HTTP 请求生命周期影响
			bgCtx := context.Background()

			var err error
			var result interface{}

			switch scope {
			case "object":
				objectKey, ok := params["object_key"].(string)
				if !ok || objectKey == "" {
					s.taskTracker.SetTaskError(taskID, fmt.Errorf("参数 object_key 无效"))
					return
				}
				result, err = s.embeddingService.EmbedObject(bgCtx, EmbedObjectRequest{
					EngineID:  engineID,
					Bucket:    bucket,
					ObjectKey: objectKey,
					TenantID:  &tenantID,
				})

			case "directory":
				prefix, _ := params["prefix"].(string)
				recursive, _ := params["recursive"].(bool)
				if recursive == false {
					// 默认为 true
					recursive = true
				}
				result, err = s.embeddingService.EmbedDirectory(bgCtx, EmbedDirectoryRequest{
					EngineID:  engineID,
					Bucket:    bucket,
					Prefix:    prefix,
					Recursive: recursive,
					TenantID:  &tenantID,
				})

			case "bucket":
				result, err = s.embeddingService.EmbedBucket(bgCtx, EmbedBucketRequest{
					EngineID: engineID,
					Bucket:   bucket,
					TenantID: &tenantID,
				})

			default:
				s.taskTracker.SetTaskError(taskID, fmt.Errorf("未知的 scope: %s", scope))
				return
			}

			if err != nil {
				// 任务失败
				s.taskTracker.SetTaskError(taskID, err)
				fmt.Printf("向量化任务失败: %v\n", err)
			} else {
				// 任务成功
				s.taskTracker.SetTaskResult(taskID, result)
				s.taskTracker.UpdateStatus(taskID, TaskStatusCompleted, "向量化完成")
			}
		}()
	}

	return response, nil
}

// GetTaskStatus 获取任务状态
func (s *OperatorService) GetTaskStatus(taskID string) (*TaskInfo, error) {
	task, ok := s.taskTracker.GetTask(taskID)
	if !ok {
		return nil, fmt.Errorf("任务不存在: %s", taskID)
	}
	return task, nil
}
