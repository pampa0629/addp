package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/logger"
	"github.com/addp/develop/backend/internal/models"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	_ "github.com/addp/common/engine/plugins/builtin/general"
)

// NotebookExecutionService Notebook 执行服务
type NotebookExecutionService struct {
	minioClient    *minio.Client
	jupyterService *JupyterService
}

// NewNotebookExecutionService 创建 Notebook 执行服务
func NewNotebookExecutionService(jupyterService *JupyterService) (*NotebookExecutionService, error) {
	if jupyterService == nil {
		return nil, fmt.Errorf("Jupyter service is required")
	}
	minioCfg := commonConfig.LoadBuiltinMinIOConfig()

	// 初始化 MinIO 客户端
	minioClient, err := minio.New(minioCfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(minioCfg.AccessKey, minioCfg.SecretKey, ""),
		Secure: minioCfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MinIO client: %w", err)
	}

	return &NotebookExecutionService{
		minioClient: minioClient, jupyterService: jupyterService,
	}, nil
}

// NotebookExecutionResult Notebook 执行结果
type NotebookExecutionResult struct {
	Status            string                   `json:"status"`             // 'success' | 'failed'
	OutputPath        string                   `json:"output_path"`        // MinIO 路径
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

	// 1. 从 content 获取 notebook_path
	notebookPath, ok := devTask.Content["notebook_path"].(string)
	if !ok || notebookPath == "" {
		return nil, "", fmt.Errorf("notebook_path not found in dev_task content")
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
		"notebook_path", notebookPath)

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
			TenantID: devTask.TenantID, NotebookPath: notebookPath,
			Parameters: parameters, Kernel: kernel,
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
		OutputPath:        execResp.OutputPath,
		CellCount:         execResp.CellCount,
		ExecutionCount:    execResp.ExecutionCount,
		OutputsPreview:    execResp.Outputs,
		VariablesExported: make(map[string]string), // TODO: 从 Jupyter Engine 返回
		ExecutionTimeMs:   executionTime,
	}

	logger.L().Info("Notebook 执行成功",
		"execution_id", executionID,
		"output_path", execResp.OutputPath,
		"execution_time_ms", executionTime)

	return result, "", nil
}

// ReadNotebookFromMinIO 从 MinIO 读取 Notebook 文件
func (s *NotebookExecutionService) ReadNotebookFromMinIO(ctx context.Context, path string) ([]byte, error) {
	bucketName := "develop"

	logger.L().Info("从 MinIO 读取 Notebook",
		"bucket", bucketName,
		"path", path)

	object, err := s.minioClient.GetObject(ctx, bucketName, path, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get object from MinIO: %w", err)
	}
	defer object.Close()

	// 读取文件内容
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, object); err != nil {
		return nil, fmt.Errorf("failed to read object content: %w", err)
	}

	return buf.Bytes(), nil
}

// SaveNotebookToMinIO 保存 Notebook 到 MinIO
func (s *NotebookExecutionService) SaveNotebookToMinIO(ctx context.Context, path string, content []byte) error {
	bucketName := "develop"

	logger.L().Info("保存 Notebook 到 MinIO",
		"bucket", bucketName,
		"path", path,
		"size_bytes", len(content))

	_, err := s.minioClient.PutObject(
		ctx,
		bucketName,
		path,
		bytes.NewReader(content),
		int64(len(content)),
		minio.PutObjectOptions{
			ContentType: "application/json",
		},
	)
	if err != nil {
		return fmt.Errorf("failed to put object to MinIO: %w", err)
	}

	return nil
}

func (s *NotebookExecutionService) DeleteNotebookFromMinIO(ctx context.Context, path string) error {
	if err := s.minioClient.RemoveObject(ctx, "develop", path, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("failed to remove object from MinIO: %w", err)
	}
	return nil
}

// ToMap 将执行结果转换为 map（用于写入统一执行记录的结果摘要）
func (r *NotebookExecutionResult) ToMap() map[string]interface{} {
	data, _ := json.Marshal(r)
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	return result
}
