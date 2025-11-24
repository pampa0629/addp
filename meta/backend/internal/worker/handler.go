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
	scanTaskService *service.ScanTaskService
}

// NewTaskHandler 创建任务处理器
func NewTaskHandler(scanTaskService *service.ScanTaskService) *TaskHandler {
	return &TaskHandler{
		scanTaskService: scanTaskService,
	}
}

// HandleScanTask 处理扫描任务
func (h *TaskHandler) HandleScanTask(ctx context.Context, t *asynq.Task) error {
	var payload ScanTaskPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("无法解析任务载荷: %w", err)
	}

	log.Printf("🔄 开始执行扫描任务 - RunID: %d, TaskID: %d, TenantID: %d",
		payload.RunID, payload.TaskID, payload.TenantID)

	// 执行扫描任务
	err := h.scanTaskService.ExecuteScanRun(ctx, payload.RunID)
	if err != nil {
		log.Printf("❌ 扫描任务执行失败 - RunID: %d, Error: %v", payload.RunID, err)
		return fmt.Errorf("扫描任务执行失败: %w", err)
	}

	log.Printf("✅ 扫描任务执行成功 - RunID: %d", payload.RunID)
	return nil
}

// RegisterHandlers 注册所有任务处理器
func (h *TaskHandler) RegisterHandlers(mux *asynq.ServeMux) {
	mux.HandleFunc(TypeScanTask, h.HandleScanTask)
}
