package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/addp/meta/internal/service"
	"github.com/hibiken/asynq"
)

// TaskHandler Worker 任务处理器
type TaskHandler struct {
	executionService *service.ScanExecutionService
}

// NewTaskHandler 创建任务处理器
func NewTaskHandler(executionService *service.ScanExecutionService) *TaskHandler {
	return &TaskHandler{
		executionService: executionService,
	}
}

// HandleScanTask 处理扫描任务
func (h *TaskHandler) HandleScanTask(ctx context.Context, t *asynq.Task) error {
	var payload ScanTaskPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("无法解析任务载荷: %w", err)
	}

	log.Printf("🔄 开始执行扫描任务 - ExecutionID: %s, TaskID: %d, TenantID: %d",
		payload.ExecutionID, payload.TaskID, payload.TenantID)

	// 执行扫描任务
	err := h.executionService.ExecuteScanRun(ctx, payload.ExecutionID)
	if err != nil {
		log.Printf("❌ 扫描任务执行失败 - ExecutionID: %s, Error: %v", payload.ExecutionID, err)
		return fmt.Errorf("扫描任务执行失败: %w", err)
	}

	log.Printf("✅ 扫描任务执行成功 - ExecutionID: %s", payload.ExecutionID)
	return nil
}

// RegisterHandlers 注册所有任务处理器
func (h *TaskHandler) RegisterHandlers(mux *asynq.ServeMux) {
	mux.HandleFunc(TypeScanTask, h.HandleScanTask)
}
