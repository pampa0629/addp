package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/logger"
	"github.com/addp/common/runtimeconn"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/models"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	_ "github.com/addp/common/engine/plugins/clickhouse"
	_ "github.com/addp/common/engine/plugins/doris"
	_ "github.com/addp/common/engine/plugins/minio"
	_ "github.com/addp/common/engine/plugins/mongodb"
	_ "github.com/addp/common/engine/plugins/mysql"
	_ "github.com/addp/common/engine/plugins/neo4j"
	_ "github.com/addp/common/engine/plugins/nfs"
	_ "github.com/addp/common/engine/plugins/postgresql"
	_ "github.com/addp/common/engine/plugins/s3"
	_ "github.com/addp/common/engine/plugins/spark_sql"
)

// NotebookExecutionService Notebook 执行服务
type NotebookExecutionService struct {
	cfg            *config.Config
	minioClient    *minio.Client
	systemClient   *client.SystemClient
	jupyterService *JupyterService
}

// NewNotebookExecutionService 创建 Notebook 执行服务
func NewNotebookExecutionService(cfg *config.Config, systemClient *client.SystemClient, jupyterService *JupyterService) (*NotebookExecutionService, error) {
	// 从环境变量读取 MinIO 配置
	minioEndpoint := os.Getenv("MINIO_ENDPOINT")
	if minioEndpoint == "" {
		minioEndpoint = "localhost:19000" // 默认值
	}

	minioAccessKey := os.Getenv("MINIO_ROOT_USER")
	if minioAccessKey == "" {
		minioAccessKey = "minioadmin"
	}

	minioSecretKey := os.Getenv("MINIO_ROOT_PASSWORD")
	if minioSecretKey == "" {
		minioSecretKey = "minioadmin"
	}

	// 初始化 MinIO 客户端
	minioClient, err := minio.New(minioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(minioAccessKey, minioSecretKey, ""),
		Secure: false, // 开发环境不使用 SSL
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MinIO client: %w", err)
	}

	return &NotebookExecutionService{
		cfg:            cfg,
		minioClient:    minioClient,
		systemClient:   systemClient,
		jupyterService: jupyterService,
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

// ExecuteNotebook 执行 Notebook（来自 dev_items）
func (s *NotebookExecutionService) ExecuteNotebook(
	ctx context.Context,
	devItem *models.DevTask,
	executionID string,
	userID uint,
) (*NotebookExecutionResult, string, error) {
	startTime := time.Now()

	// 1. 从 content 获取 notebook_path
	notebookPath, ok := devItem.Content["notebook_path"].(string)
	if !ok || notebookPath == "" {
		return nil, "", fmt.Errorf("notebook_path not found in dev_item content")
	}

	// 获取参数（可选）
	parameters, _ := devItem.Content["parameters"].(map[string]interface{})
	if parameters == nil {
		parameters = make(map[string]interface{})
	}

	// 获取数据源 IDs（可选）
	var dataSourceIDs []uint
	if dsIDsRaw, ok := devItem.Content["data_sources"].([]interface{}); ok {
		for _, id := range dsIDsRaw {
			if idFloat, ok := id.(float64); ok {
				dataSourceIDs = append(dataSourceIDs, uint(idFloat))
			}
		}
	}

	// 获取 kernel（默认 python3）
	kernel, _ := devItem.Content["kernel"].(string)
	if kernel == "" {
		kernel = "python3"
	}

	logger.L().Info("开始执行 Notebook",
		"dev_item_id", devItem.ID,
		"execution_id", executionID,
		"notebook_path", notebookPath,
		"data_sources_count", len(dataSourceIDs))

	// 2. 准备数据源连接对象（预注入）
	connections, err := s.PrepareDataSourceConnections(ctx, dataSourceIDs)
	if err != nil {
		logger.L().Warn("部分数据源连接准备失败，继续执行", "error", err)
		// 不返回错误，允许 Notebook 在没有某些连接的情况下运行
	}

	// 3. 合并参数（连接对象 + 用户参数）
	enrichedParams := make(map[string]interface{})
	for k, v := range connections {
		enrichedParams[k] = v
	}
	for k, v := range parameters {
		enrichedParams[k] = v
	}

	logger.L().Info("准备执行 Notebook",
		"connections_count", len(connections),
		"user_params_count", len(parameters))

	// 4. 调用 Jupyter Engine API 执行（使用新架构：直接传递 tenant_id + notebook_path）
	// 注意：这里调用的是新的 Jupyter Engine API (/api/execute)
	// 它接受 tenant_id 和 notebook_path，会自动从 MinIO 下载和上传
	timeout := devItem.Timeout
	if timeout <= 0 {
		timeout = 600 // 默认 10 分钟
	}

	// 调用新的 Jupyter Engine API
	execResp, err := s.callJupyterEngineAPI(ctx, devItem.TenantID, notebookPath, enrichedParams, kernel, timeout)
	if err != nil {
		executionTime := time.Since(startTime).Milliseconds()
		return &NotebookExecutionResult{
			Status:          "failed",
			ErrorMessage:    err.Error(),
			ExecutionTimeMs: executionTime,
		}, "", fmt.Errorf("jupyter execution failed: %w", err)
	}

	// 5. 构造结果
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

// PrepareDataSourceConnections 准备数据源连接对象（预注入）
func (s *NotebookExecutionService) PrepareDataSourceConnections(
	ctx context.Context,
	engineIDs []uint,
) (map[string]interface{}, error) {
	connections := make(map[string]interface{})

	for _, engineID := range engineIDs {
		// 1. 从 System 模块获取引擎配置
		engine, err := s.systemClient.GetEngine(engineID)
		if err != nil {
			logger.L().Warn("获取引擎失败，跳过",
				"engine_id", engineID,
				"error", err)
			continue
		}

		connInfo, err := runtimeconn.BuildNotebookConnection(engine, runtimeconn.ExportOptions{})
		if err != nil {
			logger.L().Warn("不支持的引擎类型，跳过",
				"engine_id", engineID,
				"engine_type", engine.EngineType,
				"error", err)
			continue
		}

		// 3. 使用引擎 ID 作为变量名（Notebook 中可以直接使用）
		varName := fmt.Sprintf("ds_%d", engineID) // 如 ds_5
		connections[varName] = connInfo

		logger.L().Info("准备数据源连接",
			"engine_id", engineID,
			"engine_type", engine.EngineType,
			"var_name", varName)
	}

	return connections, nil
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

// GenerateOutputPath 生成执行输出路径（租户级别隔离）
func (s *NotebookExecutionService) GenerateOutputPath(tenantID uint, userID uint, executionID string) string {
	// 路径格式：tenant_<id>/executions/exec_<execution_id>.ipynb
	return fmt.Sprintf("tenant_%d/executions/exec_%s.ipynb", tenantID, executionID)
}

// GenerateTempPath 生成临时路径（租户级别隔离）
func (s *NotebookExecutionService) GenerateTempPath(tenantID uint, userID uint, executionID string, suffix string) string {
	// 路径格式：tenant_<id>/temp/exec_<execution_id>_<suffix>.ipynb
	return fmt.Sprintf("tenant_%d/temp/exec_%s_%s.ipynb", tenantID, executionID, suffix)
}

// ToMap 将执行结果转换为 map（用于保存到 dev_executions.result）
func (r *NotebookExecutionResult) ToMap() map[string]interface{} {
	data, _ := json.Marshal(r)
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	return result
}

// GenerateExecutionID 生成唯一的执行 ID
func GenerateExecutionID() string {
	return uuid.New().String()
}

// callJupyterEngineAPI 调用 Jupyter Engine API 执行 Notebook
// 使用新架构：直接传递 tenant_id + notebook_path
// Jupyter Engine 会自动从 MinIO 下载、执行、上传结果
func (s *NotebookExecutionService) callJupyterEngineAPI(
	ctx context.Context,
	tenantID uint,
	notebookPath string,
	parameters map[string]interface{},
	kernel string,
	timeoutSeconds int,
) (*JupyterExecuteResponse, error) {
	// 构造请求体
	requestBody := map[string]interface{}{
		"tenant_id":     tenantID,
		"notebook_path": notebookPath,
		"parameters":    parameters,
		"kernel":        kernel,
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// 发送请求到 Jupyter Engine
	jupyterURL := s.cfg.JupyterEngineURL
	if jupyterURL == "" {
		jupyterURL = "http://localhost:8097" // 默认值
	}

	url := fmt.Sprintf("%s/api/execute", jupyterURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	logger.L().Info("调用 Jupyter Engine API",
		"url", url,
		"tenant_id", tenantID,
		"notebook_path", notebookPath,
		"parameters_count", len(parameters))

	// 创建带超时的 HTTP 客户端
	client := &http.Client{
		Timeout: time.Duration(timeoutSeconds+60) * time.Second, // 加 60 秒缓冲
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jupyter engine returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// 解析响应
	var result struct {
		Status               string                   `json:"status"`
		Message              string                   `json:"message"`
		ExecutionTimeSeconds float64                  `json:"execution_time_seconds"`
		OutputPath           string                   `json:"output_path"`
		CellCount            int                      `json:"cell_count"`
		ExecutionCount       int                      `json:"execution_count"`
		OutputCount          int                      `json:"output_count"`
		Outputs              []map[string]interface{} `json:"outputs"`
		VariablesExported    map[string]string        `json:"variables_exported"`
		ErrorMessage         string                   `json:"error_message"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// 检查执行状态
	if result.Status == "error" {
		return nil, fmt.Errorf("notebook execution failed: %s", result.ErrorMessage)
	}

	logger.L().Info("Jupyter Engine 执行成功",
		"output_path", result.OutputPath,
		"execution_time_seconds", result.ExecutionTimeSeconds)

	// 转换为 JupyterExecuteResponse 格式（兼容现有代码）
	return &JupyterExecuteResponse{
		Status:               result.Status,
		Message:              result.Message,
		ExecutionTimeSeconds: result.ExecutionTimeSeconds,
		OutputPath:           result.OutputPath,
		CellCount:            result.CellCount,
		ExecutionCount:       result.ExecutionCount,
		OutputCount:          result.OutputCount,
		Outputs:              result.Outputs,
		ErrorMessage:         result.ErrorMessage,
	}, nil
}

// JupyterExecuteResponse Jupyter Engine 执行响应（兼容现有代码）
type JupyterExecuteResponse struct {
	Status               string                   `json:"status"`
	Message              string                   `json:"message"`
	ExecutionTimeSeconds float64                  `json:"execution_time_seconds"`
	OutputPath           string                   `json:"output_path"`
	CellCount            int                      `json:"cell_count"`
	ExecutionCount       int                      `json:"execution_count"`
	OutputCount          int                      `json:"output_count"`
	Outputs              []map[string]interface{} `json:"outputs"`
	ErrorMessage         string                   `json:"error_message"`
}
