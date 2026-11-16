package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/addp/common/infra/objectstore"
	"github.com/addp/common/logger"
	"github.com/addp/develop/backend/internal/models"
)

// NotebookExecutionService Notebook 执行服务
type NotebookExecutionService struct {
	store          objectstore.Area
	jupyterService *JupyterService
}

// NewNotebookExecutionService 创建 Notebook 执行服务
func NewNotebookExecutionService(jupyterService *JupyterService, store objectstore.Area) (*NotebookExecutionService, error) {
	if jupyterService == nil {
		return nil, fmt.Errorf("Jupyter service is required")
	}
	return &NotebookExecutionService{store: store, jupyterService: jupyterService}, nil
}

// NotebookExecutionResult Notebook 执行结果
type NotebookExecutionResult struct {
	Status            string                   `json:"status"`             // 'success' | 'failed'
	OutputLocator     string                   `json:"output_locator"`     // 平台对象引用
	CellCount         int                      `json:"cell_count"`         // Cell 总数
	ExecutionCount    int                      `json:"execution_count"`    // 执行的 Cell 数
	OutputsPreview    []map[string]interface{} `json:"outputs_preview"`    // 前 5 个输出预览
	VariablesExported map[string]string        `json:"variables_exported"` // 导出的变量
	ErrorMessage      string                   `json:"error_message,omitempty"`
	ExecutionTimeMs   int64                    `json:"execution_time_ms"` // 执行时间（毫秒）
}

// ExecuteNotebook 执行 Notebook（来自 develop.dev_tasks）
func (s *NotebookExecutionService) ExecuteNotebook(
	ctx context.Context,
	devTask *models.DevTask,
	executionID string,
) (*NotebookExecutionResult, string, error) {
	startTime := time.Now()

	// 1. 从 content 获取 notebook_locator
	notebookPath, ok := devTask.Content["notebook_locator"].(string)
	if !ok || notebookPath == "" {
		return nil, "", fmt.Errorf("notebook_locator not found in dev_task content")
	}

	// 获取参数（可选）
	parameters, _ := devTask.Content["parameters"].(map[string]interface{})
	if parameters == nil {
		parameters = make(map[string]interface{})
	}

	// 获取 kernel（默认 python3）
	kernel, _ := devTask.Content["kernel"].(string)
	if kernel == "" {
		kernel = "python3"
	}

	logger.L().Info("开始执行 Notebook",
		"source_task_id", devTask.ID,
		"execution_id", executionID,
		"notebook_locator", notebookPath)

	logger.L().Info("准备执行 Notebook",
		"user_params_count", len(parameters))

	// 2. 调用 Jupyter Runtime API 执行
	timeout := devTask.Timeout
	if timeout <= 0 {
		timeout = 600 // 默认 10 分钟
	}

	engineID := devTask.GetEngineID()
	if engineID == nil {
		return nil, "", fmt.Errorf("script task must provide execution_config.engine_id")
	}
	execResp, err := s.jupyterService.ExecuteNotebook(
		ctx,
		devTask.TenantID,
		*engineID,
		JupyterRuntimeExecutionRequest{
			TenantID: devTask.TenantID, NotebookLocator: notebookPath,
			NotebookContentSHA256: notebookContentDigest(devTask),
			Parameters:            parameters, Kernel: kernel,
		},
		time.Duration(timeout)*time.Second,
	)
	if err != nil {
		executionTime := time.Since(startTime).Milliseconds()
		return &NotebookExecutionResult{
			Status:          "failed",
			ErrorMessage:    err.Error(),
			ExecutionTimeMs: executionTime,
		}, "", fmt.Errorf("jupyter execution failed: %w", err)
	}

	// 3. 构造结果
	executionTime := time.Since(startTime).Milliseconds()
	result := &NotebookExecutionResult{
		Status:            "success",
		OutputLocator:     execResp.OutputLocator,
		CellCount:         execResp.CellCount,
		ExecutionCount:    execResp.ExecutionCount,
		OutputsPreview:    execResp.Outputs,
		VariablesExported: make(map[string]string), // TODO: 从 Jupyter Engine 返回
		ExecutionTimeMs:   executionTime,
	}

	logger.L().Info("Notebook 执行成功",
		"execution_id", executionID,
		"output_locator", execResp.OutputLocator,
		"execution_time_ms", executionTime)

	return result, "", nil
}

// ReadNotebook reads the exact deployment-bound object selected by the owner.
func (s *NotebookExecutionService) ReadNotebook(ctx context.Context, tenantID uint, locator string) ([]byte, error) {
	ref, err := notebookRef(tenantID, locator)
	if err != nil {
		return nil, err
	}
	stream, err := s.store.Open(ctx, ref.Key)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	return io.ReadAll(stream)
}
func (s *NotebookExecutionService) SaveNotebook(ctx context.Context, tenantID uint, locator string, content []byte) error {
	ref, err := notebookRef(tenantID, locator)
	if err != nil {
		return err
	}
	return s.store.Put(ctx, ref.Key, bytes.NewReader(content), int64(len(content)), "application/x-ipynb+json")
}
func (s *NotebookExecutionService) DeleteNotebook(ctx context.Context, tenantID uint, locator string) error {
	ref, err := notebookRef(tenantID, locator)
	if err != nil {
		return err
	}
	return s.store.Delete(ctx, ref.Key)
}

// ToMap 将执行结果转换为 map（用于写入统一执行记录的结果摘要）
func (r *NotebookExecutionResult) ToMap() map[string]interface{} {
	data, _ := json.Marshal(r)
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	return result
}

// FreezeContent records the bytes selected at submission; the runtime rechecks its own download.
func (s *NotebookExecutionService) FreezeContent(ctx context.Context, task *models.DevTask) error {
	if task == nil || task.TenantID == 0 || s == nil || s.store == nil {
		return fmt.Errorf("Notebook content store is unavailable")
	}
	notebookPath, _ := task.Content["notebook_locator"].(string)
	content, err := s.ReadNotebook(ctx, task.TenantID, notebookPath)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(content)
	config := models.DevTaskContent{}
	for key, value := range task.ExecutionConfig {
		config[key] = value
	}
	config["notebook_content_sha256"] = hex.EncodeToString(digest[:])
	task.ExecutionConfig = config
	return nil
}

func notebookRef(tenantID uint, locator string) (objectstore.Ref, error) {
	ref, err := objectstore.Parse(locator)
	if err != nil || ref.Area != objectstore.Develop || ref.Type != "object" ||
		!strings.HasPrefix(ref.Key, fmt.Sprintf("tenant_%d/notebooks/", tenantID)) || !strings.HasSuffix(ref.Key, ".ipynb") {
		return objectstore.Ref{}, ErrNotebookFileIdentity
	}
	return ref, nil
}

func notebookContentDigest(task *models.DevTask) string {
	value, _ := task.ExecutionConfig["notebook_content_sha256"].(string)
	return value
}
