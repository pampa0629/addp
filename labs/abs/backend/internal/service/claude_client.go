package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ClaudeClient handles communication with Claude API
type ClaudeClient struct {
	apiKey    string
	authToken string
	baseURL   string
	model     string
	client    *http.Client
}

// NewClaudeClient creates a new Claude API client
func NewClaudeClient(apiKey, authToken, baseURL, model string) *ClaudeClient {
	return &ClaudeClient{
		apiKey:    apiKey,
		authToken: authToken,
		baseURL:   baseURL,
		model:     model,
		client:    &http.Client{},
	}
}

// ClaudeContentBlock represents a block of content in Claude API format
type ClaudeContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ClaudeMessage represents a message in Claude API format
type ClaudeMessage struct {
	Role    string               `json:"role"`
	Content []ClaudeContentBlock `json:"content"`
}

// ClaudeRequest represents a request to Claude API
type ClaudeRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    string          `json:"system,omitempty"`
	Messages  []ClaudeMessage `json:"messages"`
}

// ClaudeResponse represents a response from Claude API
type ClaudeResponse struct {
	ID      string               `json:"id"`
	Type    string               `json:"type"`
	Role    string               `json:"role"`
	Content []ClaudeContentBlock `json:"content"`
}

// GenerateCode sends a prompt to Claude and returns generated code
func (c *ClaudeClient) GenerateCode(prompt string) (string, error) {
	// Construct the system prompt for code generation
	systemPrompt := `You are an expert code generator. Generate clean, working code based on the user's request.
Return ONLY the code without any explanations or markdown formatting.
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
(code here)

// filepath: src/utils.go
(code here)`

	reqBody := ClaudeRequest{
		Model:     c.model,
		MaxTokens: 4096,
		System:    systemPrompt,
		Messages: []ClaudeMessage{
			{
				Role: "user",
				Content: []ClaudeContentBlock{
					{
						Type: "text",
						Text: prompt,
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// For Claude Code proxy, use auth token as primary authentication
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	} else {
		req.Header.Set("x-api-key", c.apiKey)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("User-Agent", "Claude-Code-CLI/1.0.0")
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var claudeResp ClaudeResponse
	if err := json.NewDecoder(resp.Body).Decode(&claudeResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	var builder strings.Builder
	for _, block := range claudeResp.Content {
		if block.Type == "text" {
			builder.WriteString(block.Text)
		}
	}

	code := builder.String()
	if strings.TrimSpace(code) == "" {
		return "", fmt.Errorf("empty response from Claude")
	}

	return code, nil
}

func (c *ClaudeClient) Provider() string {
	return "claude"
}
