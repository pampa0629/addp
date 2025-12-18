package service

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/common/models"
	"github.com/addp/transfer/internal/operators"
	transferModels "github.com/addp/transfer/internal/models"
)

// OperatorService 算子服务
type OperatorService struct {
	taskService *TaskService
}

// NewOperatorService 创建算子服务
func NewOperatorService(taskService *TaskService) *OperatorService {
	return &OperatorService{
		taskService: taskService,
	}
}

// GetOperators 获取所有算子
func (s *OperatorService) GetOperators() []models.OperatorMetadata {
	return operators.TransferOperators
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
	case "batch_transfer":
		return s.executeBatchTransferOperator(ctx, tenantID, userID, params, executeNow, taskName)
	case "stream_transfer":
		return s.executeStreamTransferOperator(ctx, tenantID, userID, params, executeNow, taskName)
	default:
		return nil, fmt.Errorf("未知的算子: %s", operatorName)
	}
}

// executeBatchTransferOperator 执行批量传输算子
func (s *OperatorService) executeBatchTransferOperator(
	ctx context.Context,
	tenantID uint,
	userID uint,
	params map[string]interface{},
	executeNow bool,
	taskName string,
) (*models.OperatorExecuteResponse, error) {

	// 解析参数
	sourceID, ok := params["source_id"].(float64)
	if !ok {
		return nil, fmt.Errorf("参数 source_id 必须为数字")
	}

	targetID, ok := params["target_id"].(float64)
	if !ok {
		return nil, fmt.Errorf("参数 target_id 必须为数字")
	}

	taskType, ok := params["task_type"].(string)
	if !ok {
		taskType = "sync" // 默认值
	}

	batchSize := 1000 // 默认值
	if bs, ok := params["batch_size"].(float64); ok {
		batchSize = int(bs)
	}

	maxParallelism := 1 // 默认值
	if mp, ok := params["max_parallelism"].(float64); ok {
		maxParallelism = int(mp)
	}

	config, ok := params["config"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("参数 config 必须为对象")
	}

	// 构建任务名称
	if taskName == "" {
		taskName = fmt.Sprintf("批量传输-%d到%d", int(sourceID), int(targetID))
	}

	// 构建源和目标ID的指针
	sourceIDPtr := uint(sourceID)
	targetIDPtr := uint(targetID)

	// 创建传输任务请求
	taskReq := &transferModels.CreateTaskRequest{
		Name:           taskName,
		Type:           transferModels.TaskType(taskType),
		Mode:           transferModels.TaskModeBatch,
		SourceID:       &sourceIDPtr,
		TargetID:       &targetIDPtr,
		Config:         config,
		BatchSize:      batchSize,
		MaxParallelism: maxParallelism,
	}

	createdTask, err := s.taskService.CreateTask(ctx, taskReq, tenantID, userID)
	if err != nil {
		return nil, err
	}

	response := &models.OperatorExecuteResponse{
		Status:     "success",
		TaskID:     fmt.Sprintf("%d", createdTask.ID),
		TaskStatus: "pending",
		Message:    "传输任务已创建",
		CreatedAt:  createdTask.CreatedAt.Format(time.RFC3339),
	}

	// 如果是立即执行
	if executeNow {
		// 触发任务执行(通过StartTask方法)
		_, err = s.taskService.StartTask(ctx, createdTask.ID, tenantID, userID)
		if err != nil {
			return nil, err
		}
		response.TaskStatus = "running"
		response.Message = "传输任务已创建并开始执行"
	}

	return response, nil
}

// executeStreamTransferOperator 执行流式传输算子
func (s *OperatorService) executeStreamTransferOperator(
	ctx context.Context,
	tenantID uint,
	userID uint,
	params map[string]interface{},
	executeNow bool,
	taskName string,
) (*models.OperatorExecuteResponse, error) {

	// 解析参数
	sourceID, ok := params["source_id"].(float64)
	if !ok {
		return nil, fmt.Errorf("参数 source_id 必须为数字")
	}

	targetID, ok := params["target_id"].(float64)
	if !ok {
		return nil, fmt.Errorf("参数 target_id 必须为数字")
	}

	config, ok := params["config"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("参数 config 必须为对象")
	}

	// 构建任务名称
	if taskName == "" {
		taskName = fmt.Sprintf("流式传输-%d到%d", int(sourceID), int(targetID))
	}

	// 构建源和目标ID的指针
	sourceIDPtr := uint(sourceID)
	targetIDPtr := uint(targetID)

	// 创建流式传输任务请求
	taskReq := &transferModels.CreateTaskRequest{
		Name:     taskName,
		Type:     transferModels.TaskTypeSync,
		Mode:     transferModels.TaskModeStream,
		SourceID: &sourceIDPtr,
		TargetID: &targetIDPtr,
		Config:   config,
	}

	createdTask, err := s.taskService.CreateTask(ctx, taskReq, tenantID, userID)
	if err != nil {
		return nil, err
	}

	response := &models.OperatorExecuteResponse{
		Status:     "success",
		TaskID:     fmt.Sprintf("%d", createdTask.ID),
		TaskStatus: "pending",
		Message:    "流式传输任务已创建",
		CreatedAt:  createdTask.CreatedAt.Format(time.RFC3339),
	}

	// 如果是立即执行
	if executeNow {
		_, err = s.taskService.StartTask(ctx, createdTask.ID, tenantID, userID)
		if err != nil {
			return nil, err
		}
		response.TaskStatus = "running"
		response.Message = "流式传输任务已创建并开始执行"
	}

	return response, nil
}
