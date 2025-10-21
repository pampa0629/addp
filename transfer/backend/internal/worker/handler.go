package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/addp/transfer/internal/service"
	"github.com/hibiken/asynq"
)

// TaskHandler Worker 任务处理器
type TaskHandler struct {
	taskService      *service.TaskService
	executionService *service.ExecutionService
}

// NewTaskHandler 创建任务处理器
func NewTaskHandler(taskService *service.TaskService, executionService *service.ExecutionService) *TaskHandler {
	return &TaskHandler{
		taskService:      taskService,
		executionService: executionService,
	}
}

// HandleExecuteTask 处理任务执行
func (h *TaskHandler) HandleExecuteTask(ctx context.Context, t *asynq.Task) error {
	var payload ExecuteTaskPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("无法解析任务载荷: %w", err)
	}

	log.Printf("🔄 开始执行任务 - TaskID: %d, ExecutionID: %d, TenantID: %d",
		payload.TaskID, payload.ExecutionID, payload.TenantID)

	// 执行任务
	err := h.taskService.ExecuteTask(ctx, payload.TaskID, payload.ExecutionID)
	if err != nil {
		log.Printf("❌ 任务执行失败 - TaskID: %d, Error: %v", payload.TaskID, err)
		return fmt.Errorf("任务执行失败: %w", err)
	}

	log.Printf("✅ 任务执行成功 - TaskID: %d, ExecutionID: %d", payload.TaskID, payload.ExecutionID)
	return nil
}

// RegisterHandlers 注册所有任务处理器
func (h *TaskHandler) RegisterHandlers(mux *asynq.ServeMux) {
	mux.HandleFunc(TypeExecuteTask, h.HandleExecuteTask)
}
