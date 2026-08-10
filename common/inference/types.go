package inference

import "encoding/json"

const SchemaVersion = "addp.inference/v1"

const (
	OperationChat      = "chat"
	OperationEmbedding = "embedding"
	OperationRerank    = "rerank"
	ModalityText       = "text"
	ModalityImage      = "image"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ResponseSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema"`
	Strict      bool            `json:"strict"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

type ResolveProfileRequest struct {
	SchemaVersion  string `json:"schema_version"`
	TenantID       uint   `json:"tenant_id"`
	ModelProfileID string `json:"model_profile_id"`
	Operation      string `json:"operation"`
	Modality       string `json:"modality"`
	CallerToken    string `json:"-"`
}

type ResolveProfileResponse struct {
	SchemaVersion  string `json:"schema_version"`
	ModelProfileID string `json:"model_profile_id"`
	ProfileVersion int64  `json:"profile_version"`
	DeploymentID   string `json:"deployment_id"`
	Dimension      int    `json:"dimension"`
}

type ChatRequest struct {
	SchemaVersion   string           `json:"schema_version"`
	TenantID        uint             `json:"tenant_id"`
	ModelProfileID  string           `json:"model_profile_id"`
	Messages        []Message        `json:"messages"`
	Tools           []ToolDefinition `json:"tools,omitempty"`
	ToolChoice      string           `json:"tool_choice,omitempty"`
	ResponseSchema  *ResponseSchema  `json:"response_schema,omitempty"`
	Temperature     *float64         `json:"temperature,omitempty"`
	MaxOutputTokens int              `json:"max_output_tokens,omitempty"`
	CallerToken     string           `json:"-"`
}

type ChatResponse struct {
	SchemaVersion  string  `json:"schema_version"`
	Message        Message `json:"message"`
	Usage          Usage   `json:"usage"`
	DeploymentID   string  `json:"deployment_id"`
	ProfileVersion int64   `json:"profile_version"`
}

type EmbeddingInput struct {
	Modality string `json:"modality"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
}

type EmbeddingRequest struct {
	SchemaVersion  string           `json:"schema_version"`
	TenantID       uint             `json:"tenant_id"`
	ModelProfileID string           `json:"model_profile_id"`
	Inputs         []EmbeddingInput `json:"inputs"`
	CallerToken    string           `json:"-"`
}

type EmbeddingResponse struct {
	SchemaVersion  string      `json:"schema_version"`
	Vectors        [][]float32 `json:"vectors"`
	Dimension      int         `json:"dimension"`
	Usage          Usage       `json:"usage"`
	DeploymentID   string      `json:"deployment_id"`
	ProfileVersion int64       `json:"profile_version"`
}

type RerankDocument struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type RerankRequest struct {
	SchemaVersion  string           `json:"schema_version"`
	TenantID       uint             `json:"tenant_id"`
	ModelProfileID string           `json:"model_profile_id"`
	Query          string           `json:"query"`
	Documents      []RerankDocument `json:"documents"`
	TopN           int              `json:"top_n,omitempty"`
	CallerToken    string           `json:"-"`
}

type RerankResult struct {
	DocumentID string  `json:"document_id"`
	Index      int     `json:"index"`
	Score      float64 `json:"score"`
}

type RerankResponse struct {
	SchemaVersion  string         `json:"schema_version"`
	Results        []RerankResult `json:"results"`
	Usage          Usage          `json:"usage"`
	DeploymentID   string         `json:"deployment_id"`
	ProfileVersion int64          `json:"profile_version"`
}

type ErrorResponse struct {
	ErrorCode string `json:"error_code"`
	Error     string `json:"error"`
}
