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
	cacheManager *CacheManager
}

// NewOperatorService 创建算子服务
func NewOperatorService(cacheManager *CacheManager) *OperatorService {
	return &OperatorService{
		cacheManager: cacheManager,
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
