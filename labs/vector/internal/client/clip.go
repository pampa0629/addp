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
)

// CLIPClient CLIP 模型客户端
type CLIPClient struct {
	baseURL string
	client  *http.Client
	logger  *slog.Logger
}

// CLIPConfig 配置
type CLIPConfig struct {
	BaseURL string
	Timeout time.Duration
}

// NewCLIPClient 创建 CLIP 客户端
func NewCLIPClient(cfg CLIPConfig, logger *slog.Logger) *CLIPClient {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	return &CLIPClient{
		baseURL: cfg.BaseURL,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
		logger: logger,
	}
}

// CLIPTextRequest 文本向量化请求
type CLIPTextRequest struct {
	Text string `json:"text"`
}

// CLIPImageRequest 图片向量化请求
type CLIPImageRequest struct {
	Image string `json:"image"` // Base64 编码
}

// CLIPResponse 响应
type CLIPResponse struct {
	Embedding []float32 `json:"embedding"`
	Dimension int       `json:"dimension"`
}

// ExtractVectorFromText 从文本提取向量
func (c *CLIPClient) ExtractVectorFromText(ctx context.Context, text string) ([]float32, error) {
	c.logger.Info("[CLIP] [Stage 1] 处理输入文本", "text", text, "length", len(text))

	if text == "" {
		return nil, fmt.Errorf("文本不能为空")
	}

	// 构建请求
	reqBody := CLIPTextRequest{Text: text}
	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	c.logger.Info("[CLIP] [Stage 2] 发送向量化请求...")

	// 发送请求
	url := c.baseURL + "/embed/text"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	startTime := time.Now()
	resp, err := c.client.Do(req)
	latency := time.Since(startTime)

	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	c.logger.Info("[CLIP] [Stage 3] 响应接收",
		"status_code", resp.StatusCode,
		"latency_ms", latency.Milliseconds())

	// 解析响应
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API 错误 [%d]: %s", resp.StatusCode, string(bodyBytes))
	}

	var clipResp CLIPResponse
	if err := json.Unmarshal(bodyBytes, &clipResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	c.logger.Info("[CLIP] ✅ 文本向量化成功",
		"dimension", clipResp.Dimension,
		"first_5", fmt.Sprintf("%.4f", clipResp.Embedding[:min(5, len(clipResp.Embedding))]))

	return clipResp.Embedding, nil
}

// ExtractVectorFromImage 从图片提取向量
func (c *CLIPClient) ExtractVectorFromImage(ctx context.Context, imagePath string) ([]float32, error) {
	// Stage 1: 读取图片
	c.logger.Info("[CLIP] [Stage 1] 读取图片文件", "path", imagePath)
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, fmt.Errorf("读取图片失败: %w", err)
	}

	fileInfo, _ := os.Stat(imagePath)
	c.logger.Info("[CLIP] [Stage 1] 读取成功",
		"size_bytes", len(imageData),
		"size_kb", len(imageData)/1024)

	// Stage 2: Base64 编码
	c.logger.Info("[CLIP] [Stage 2] Base64 编码图片...")
	base64Image := base64.StdEncoding.EncodeToString(imageData)
	c.logger.Info("[CLIP] [Stage 2] 编码完成", "base64_length", len(base64Image))

	// Stage 3: 构建请求
	c.logger.Info("[CLIP] [Stage 3] 构建 API 请求")
	reqBody := CLIPImageRequest{Image: base64Image}
	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// Stage 4: 发送请求
	c.logger.Info("[CLIP] [Stage 4] 发送向量化请求...")
	url := c.baseURL + "/embed/image"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	startTime := time.Now()
	resp, err := c.client.Do(req)
	latency := time.Since(startTime)

	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	c.logger.Info("[CLIP] [Stage 4] 响应接收",
		"status_code", resp.StatusCode,
		"latency_ms", latency.Milliseconds())

	// Stage 5: 解析响应
	c.logger.Info("[CLIP] [Stage 5] 解析响应...")
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API 错误 [%d]: %s", resp.StatusCode, string(bodyBytes))
	}

	var clipResp CLIPResponse
	if err := json.Unmarshal(bodyBytes, &clipResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	// Stage 6: 提取向量
	c.logger.Info("[CLIP] [Stage 6] ✅ 图片向量化成功",
		"dimension", clipResp.Dimension,
		"first_5", fmt.Sprintf("%.4f", clipResp.Embedding[:min(5, len(clipResp.Embedding))]))

	_ = fileInfo // 避免未使用变量警告

	return clipResp.Embedding, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
