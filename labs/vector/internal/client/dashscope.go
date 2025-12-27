package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/addp/labs/vector/internal/models"
)

// DashScope API 常量
const (
	DefaultTimeout = 30 * time.Second
)

// DashScopeClient DashScope API 客户端
type DashScopeClient struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
	logger  *slog.Logger
}

// DashScopeConfig 配置结构
type DashScopeConfig struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
}

// NewDashScopeClient 创建客户端
func NewDashScopeClient(cfg DashScopeConfig, logger *slog.Logger) *DashScopeClient {
	if cfg.Model == "" {
		cfg.Model = models.ModelTongyiEmbeddingVisionPlus
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}

	return &DashScopeClient{
		baseURL: cfg.BaseURL,
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
		logger: logger,
	}
}

// DashScopeRequest API 请求结构
type DashScopeRequest struct {
	Model string            `json:"model"`
	Input DashScopeInput    `json:"input"`
}

type DashScopeInput struct {
	Contents []models.ContentItem `json:"contents"`
}

// DashScopeResponse API 响应结构
type DashScopeResponse struct {
	Output struct {
		Embeddings []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"embeddings"`
	} `json:"output"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
	RequestID string `json:"request_id"`
}

// DashScopeError API 错误响应
type DashScopeError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func (e *DashScopeError) Error() string {
	return fmt.Sprintf("DashScope API error [%s]: %s (request_id: %s)",
		e.Code, e.Message, e.RequestID)
}

// ExtractVectorFromImage 从单张图片提取向量
func (c *DashScopeClient) ExtractVectorFromImage(ctx context.Context, imagePath string) ([]float32, error) {
	// Stage 1: 读取图片文件
	c.logger.Info("[Stage 1] 读取图片文件", "path", imagePath)
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, fmt.Errorf("[Stage 1] 读取图片失败: %w", err)
	}
	c.logger.Info("[Stage 1] 读取成功", "size_bytes", len(imageData), "size_kb", len(imageData)/1024)

	// Stage 2: Base64 编码
	c.logger.Info("[Stage 2] Base64 编码图片...")
	base64Image := base64.StdEncoding.EncodeToString(imageData)
	c.logger.Info("[Stage 2] 编码完成", "base64_length", len(base64Image))

	// 构建内容项
	contents := []models.ContentItem{
		{Image: base64Image},
	}

	// 调用通用向量提取方法
	return c.ExtractVectorFromMultiModal(ctx, contents)
}

// ExtractVectorFromMultiModal 从多模态输入提取向量
func (c *DashScopeClient) ExtractVectorFromMultiModal(ctx context.Context, contents []models.ContentItem) ([]float32, error) {
	// Stage 3: 构建请求体
	c.logger.Info("[Stage 3] 构建 API 请求", "model", c.model, "endpoint", c.baseURL)
	reqBody := DashScopeRequest{
		Model: c.model,
		Input: DashScopeInput{
			Contents: contents,
		},
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("[Stage 3] 序列化请求失败: %w", err)
	}
	c.logger.Info("[Stage 3] 请求体构建完成", "payload_size_kb", len(reqJSON)/1024)

	// Stage 4: 发送 HTTP 请求
	c.logger.Info("[Stage 4] 发送 API 请求...")
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL, bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("[Stage 4] 创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	startTime := time.Now()
	resp, err := c.client.Do(req)
	latency := time.Since(startTime)

	if err != nil {
		return nil, fmt.Errorf("[Stage 4] API 请求失败: %w", err)
	}
	defer resp.Body.Close()

	c.logger.Info("[Stage 4] API 响应接收",
		"status_code", resp.StatusCode,
		"latency_ms", latency.Milliseconds(),
		"content_type", resp.Header.Get("Content-Type"))

	// Stage 5: 解析响应
	c.logger.Info("[Stage 5] 解析 API 响应...")
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("[Stage 5] 读取响应体失败: %w", err)
	}

	// 处理错误响应
	if resp.StatusCode != http.StatusOK {
		var apiError DashScopeError
		if err := json.Unmarshal(bodyBytes, &apiError); err != nil {
			return nil, fmt.Errorf("[Stage 5] API 返回错误 (状态码 %d): %s",
				resp.StatusCode, string(bodyBytes))
		}
		return nil, &apiError
	}

	var apiResp DashScopeResponse
	if err := json.Unmarshal(bodyBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("[Stage 5] 解析响应 JSON 失败: %w", err)
	}

	c.logger.Info("[Stage 5] 响应解析成功",
		"request_id", apiResp.RequestID,
		"total_tokens", apiResp.Usage.TotalTokens,
		"embeddings_count", len(apiResp.Output.Embeddings))

	// Stage 6: 提取向量
	c.logger.Info("[Stage 6] 提取向量数据...")
	if len(apiResp.Output.Embeddings) == 0 {
		return nil, fmt.Errorf("[Stage 6] API 未返回向量数据")
	}

	vector := apiResp.Output.Embeddings[0].Embedding
	if len(vector) == 0 {
		return nil, fmt.Errorf("[Stage 6] 向量数据为空")
	}

	// 打印前5个值用于调试
	previewCount := 5
	if previewCount > len(vector) {
		previewCount = len(vector)
	}
	preview := vector[:previewCount]

	c.logger.Info("[Stage 6] ✅ 向量提取成功",
		"dimension", len(vector),
		"first_5_values", preview)

	return vector, nil
}

// RetryWithBackoff 带指数退避的重试机制（用于处理 API 限流）
func (c *DashScopeClient) RetryWithBackoff(
	ctx context.Context,
	fn func() ([]float32, error),
	maxRetries int,
) ([]float32, error) {
	for i := 0; i < maxRetries; i++ {
		vector, err := fn()
		if err == nil {
			return vector, nil
		}

		// 检查是否是限流错误
		if apiErr, ok := err.(*DashScopeError); ok && apiErr.Code == "429" {
			if i < maxRetries-1 {
				wait := time.Duration(1<<uint(i)) * time.Second // 1s, 2s, 4s, 8s...
				c.logger.Warn("⚠️  API 限流，等待重试",
					"attempt", i+1,
					"max_retries", maxRetries,
					"wait_seconds", wait.Seconds())
				time.Sleep(wait)
				continue
			}
		}

		return nil, err
	}

	return nil, fmt.Errorf("重试 %d 次后仍失败", maxRetries)
}

// ExtractVectorFromText 从文本提取向量
func (c *DashScopeClient) ExtractVectorFromText(ctx context.Context, text string) ([]float32, error) {
	// Stage 1: 验证文本
	c.logger.Info("[Stage 1] 处理输入文本", "text", text, "length", len(text))
	if text == "" {
		return nil, fmt.Errorf("文本不能为空")
	}

	// Stage 2: 跳过（文本无需编码）
	c.logger.Info("[Stage 2] 文本模式，跳过编码")

	// Stage 3-6: 调用多模态 API
	contents := []models.ContentItem{
		{Text: text},
	}

	return c.ExtractVectorFromMultiModal(ctx, contents)
}
