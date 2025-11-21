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
	preprocessSvc   *service.PreprocessService
}

// NewTaskHandler 创建任务处理器
func NewTaskHandler(scanTaskService *service.ScanTaskService, preprocessSvc *service.PreprocessService) *TaskHandler {
	return &TaskHandler{
		scanTaskService: scanTaskService,
		preprocessSvc:   preprocessSvc,
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

// HandlePreprocessTask 处理预处理任务
func (h *TaskHandler) HandlePreprocessTask(ctx context.Context, t *asynq.Task) error {
	var payload PreprocessTaskPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("无法解析任务载荷: %w", err)
	}

	log.Printf("🔄 开始执行预处理任务 - ItemID: %d, TenantID: %d, Type: %s",
		payload.ItemID, payload.TenantID, payload.Type)

	// 执行预处理任务
	err := h.preprocessSvc.ExecutePreprocess(ctx, payload.ItemID, payload.TenantID, payload.Type)
	if err != nil {
		log.Printf("❌ 预处理任务执行失败 - ItemID: %d, Error: %v", payload.ItemID, err)
		return fmt.Errorf("预处理任务执行失败: %w", err)
	}

	log.Printf("✅ 预处理任务执行成功 - ItemID: %d", payload.ItemID)
	return nil
}

// RegisterHandlers 注册所有任务处理器
func (h *TaskHandler) RegisterHandlers(mux *asynq.ServeMux) {
	mux.HandleFunc(TypeScanTask, h.HandleScanTask)
	mux.HandleFunc(TypePreprocessTask, h.HandlePreprocessTask)
}
