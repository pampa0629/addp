package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/addp/develop/backend/internal/config"
)

// JupyterService Jupyter Notebook执行引擎服务
// 通过 HTTP API 调用 Jupyter Engine 执行 Notebook
type JupyterService struct {
	cfg               *config.Config
	jupyterEngineURL  string
	httpClient        *http.Client
}

// NewJupyterService 创建Jupyter执行引擎服务
func NewJupyterService(cfg *config.Config) *JupyterService {
	// 从环境变量或配置获取 Jupyter Engine URL
	jupyterEngineURL := cfg.JupyterEngineURL
	if jupyterEngineURL == "" {
		jupyterEngineURL = "http://jupyter-engine:8097" // 默认值
	}

	return &JupyterService{
		cfg:              cfg,
		jupyterEngineURL: jupyterEngineURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Minute, // 默认10分钟超时
		},
	}
}

// ExecuteNotebookRequest 执行Notebook请求
type ExecuteNotebookRequest struct {
	InputPath  string                 `json:"input_path"`  // 输入Notebook路径
	OutputPath string                 `json:"output_path"` // 输出Notebook路径
	Parameters map[string]interface{} `json:"parameters"`  // 参数
	Kernel     string                 `json:"kernel"`      // Kernel名称
}

// ExecuteNotebookResponse 执行Notebook响应
type ExecuteNotebookResponse struct {
	Status              string                   `json:"status"`
	Message             string                   `json:"message"`
	ExecutionTimeSeconds float64                  `json:"execution_time_seconds"`
	OutputPath          string                   `json:"output_path"`
	OutputCount         int                      `json:"output_count"`
	Outputs             []map[string]interface{} `json:"outputs"`
	ErrorMessage        string                   `json:"error_message,omitempty"`
	Traceback           string                   `json:"traceback,omitempty"`
}

// ExecuteNotebook 执行Notebook
func (s *JupyterService) ExecuteNotebook(
	ctx context.Context,
	inputPath string,
	outputPath string,
	parameters map[string]interface{},
	timeout int,
) (*ExecuteNotebookResponse, error) {
	// 设置超时
	if timeout <= 0 {
		timeout = 300 // 默认5分钟
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// 构造请求
	reqBody := ExecuteNotebookRequest{
		InputPath:  inputPath,
		OutputPath: outputPath,
		Parameters: parameters,
		Kernel:     "python3",
	}

	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// 发送HTTP请求
	apiURL := fmt.Sprintf("%s/api/execute", s.jupyterEngineURL)
	log.Printf("📓 [Jupyter Service] 执行Notebook: %s -> %s", inputPath, outputPath)

	req, err := http.NewRequestWithContext(execCtx, "POST", apiURL, bytes.NewBuffer(reqData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// 执行请求
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// 解析响应
	var result ExecuteNotebookResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// 检查状态
	if resp.StatusCode != http.StatusOK || result.Status != "success" {
		log.Printf("❌ [Jupyter Service] Notebook执行失败: %s", result.ErrorMessage)
		return &result, fmt.Errorf("notebook execution failed: %s", result.ErrorMessage)
	}

	log.Printf("✅ [Jupyter Service] Notebook执行成功 (耗时 %.2fs, 输出 %d 个)",
		result.ExecutionTimeSeconds, result.OutputCount)

	return &result, nil
}

// ListKernels 列出可用的Kernel
type KernelInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Language    string `json:"language"`
}

type ListKernelsResponse struct {
	Status  string       `json:"status"`
	Kernels []KernelInfo `json:"kernels"`
}

func (s *JupyterService) ListKernels(ctx context.Context) ([]KernelInfo, error) {
	apiURL := fmt.Sprintf("%s/api/kernels", s.jupyterEngineURL)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result ListKernelsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("failed to list kernels")
	}

	return result.Kernels, nil
}

// HealthCheck 检查Jupyter Engine健康状态
func (s *JupyterService) HealthCheck(ctx context.Context) error {
	apiURL := fmt.Sprintf("%s/health", s.jupyterEngineURL)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jupyter engine unhealthy: status code %d", resp.StatusCode)
	}

	return nil
}
