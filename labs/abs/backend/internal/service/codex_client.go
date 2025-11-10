package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// CodexClient 与 Codex/OpenAI 兼容 API 通信
type CodexClient struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

// NewCodexClient 创建 CodexClient
func NewCodexClient(apiKey, baseURL, model string) *CodexClient {
	trimmedBase := strings.TrimRight(baseURL, "/")
	if trimmedBase == "" {
		trimmedBase = "https://api.openai.com"
	}

	return &CodexClient{
		apiKey:  apiKey,
		baseURL: trimmedBase,
		model:   model,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type CodexMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type CodexRequest struct {
	Model       string         `json:"model"`
	Messages    []CodexMessage `json:"messages"`
	Temperature float64        `json:"temperature"`
	MaxTokens   int            `json:"max_tokens"`
}

type CodexChoice struct {
	Message CodexMessage `json:"message"`
}

type CodexResponse struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Choices []CodexChoice `json:"choices"`
}

// GenerateCode 调用 Codex API 生成代码
func (c *CodexClient) GenerateCode(prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("prompt cannot be empty")
	}

	if strings.TrimSpace(c.apiKey) == "" {
		return "", fmt.Errorf("Codex API key is not configured")
	}

	systemPrompt := `You are an expert coding assistant named Codex. Generate clean, working code based on the user's request.
Return ONLY the code without explanations or markdown fences.
Always include a JSON file named app_manifest.json at the project root describing how to run the generated application with fields:
{
  "name": "...",
  "description": "...",
  "category": "...",
  "icon": "...",
  "entry_url": "http://localhost:PORT[/optional_path]",
  "start_command": ["command", "args"],
  "tags": ["tag1", "tag2"],
  "port": 1234 (optional),
  "preview_path": "/optional"
}
Ensure start_command contains the exact command (split into tokens) needed to start the service from the workspace root and entry_url points to the running endpoint.
If multiple files are needed, separate them with clear file path comments like:
// filepath: src/main.go
code

// filepath: src/utils.go
code`

	reqBody := CodexRequest{
		Model:       c.model,
		MaxTokens:   4096,
		Temperature: 0.2,
		Messages: []CodexMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: prompt},
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal codex request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v1/chat/completions", c.baseURL)
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(payload))
	if err != nil {
		return "", fmt.Errorf("failed to create codex request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call codex api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("codex api error (status %d): %s", resp.StatusCode, string(body))
	}

	var codexResp CodexResponse
	if err := json.NewDecoder(resp.Body).Decode(&codexResp); err != nil {
		return "", fmt.Errorf("failed to decode codex response: %w", err)
	}

	if len(codexResp.Choices) == 0 {
		return "", fmt.Errorf("codex returned no choices")
	}

	code := strings.TrimSpace(codexResp.Choices[0].Message.Content)
	if code == "" {
		return "", fmt.Errorf("codex response is empty")
	}

	return code, nil
}

func (c *CodexClient) Provider() string {
	return "codex"
}
