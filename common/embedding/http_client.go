package embedding

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/addp/common/logger"
)

type HTTPEmbeddingClient struct {
	cfg        ServiceConfig
	httpClient *http.Client
	log        slogLogger
}

type slogLogger interface {
	Debug(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

type ClientOption func(*HTTPEmbeddingClient)

func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *HTTPEmbeddingClient) {
		if client != nil {
			c.httpClient = client
		}
	}
}

func WithLogger(l slogLogger) ClientOption {
	return func(c *HTTPEmbeddingClient) {
		if l != nil {
			c.log = l
		}
	}
}

func NewHTTPEmbeddingClient(cfg ServiceConfig, opts ...ClientOption) (*HTTPEmbeddingClient, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	client := &HTTPEmbeddingClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		log: logger.With("component", "embedding_client"),
	}

	for _, opt := range opts {
		opt(client)
	}

	return client, nil
}

type dashScopeRequest struct {
	Model string         `json:"model"`
	Input dashScopeInput `json:"input"`
}

type dashScopeInput struct {
	Contents []map[string]any `json:"contents"`
}

type dashScopeResponse struct {
	RequestID string `json:"request_id"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Output    struct {
		Embeddings []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
			Type      string    `json:"type"`
		} `json:"embeddings"`
	} `json:"output"`
	Usage struct {
		InputTokens int `json:"input_tokens"`
		ImageTokens int `json:"image_tokens"`
		VideoTokens int `json:"video_tokens"`
	} `json:"usage"`
}

type dashScopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (c *HTTPEmbeddingClient) EmbedText(ctx context.Context, inputs []TextInput) (*BatchResult, error) {
	if len(inputs) == 0 {
		return nil, ErrEmptyInput
	}

	model, err := c.cfg.ModelFor(ModalityText)
	if err != nil {
		return nil, err
	}

	return c.embedTexts(ctx, model, inputs)
}

func (c *HTTPEmbeddingClient) EmbedDocument(ctx context.Context, inputs []DocumentInput) (*BatchResult, error) {
	if len(inputs) == 0 {
		return nil, ErrEmptyInput
	}

	model, err := c.cfg.ModelFor(ModalityDocument)
	if err != nil {
		return nil, err
	}

	results := make([]Embedding, 0, len(inputs))
	var usageAcc Usage

	for _, input := range inputs {
		text := strings.TrimSpace(joinNonEmpty("\n\n", input.Title, input.Content))
		if text == "" {
			return nil, fmt.Errorf("document %s content empty", input.ID)
		}

		embedding, usage, err := c.invoke(ctx, model, []map[string]any{{"text": text}})
		if err != nil {
			return nil, err
		}
		embedding.ID = input.ID
		if embedding.Metadata == nil {
			embedding.Metadata = map[string]string{}
		}
		if input.Language != "" {
			embedding.Metadata["language"] = input.Language
		}
		results = append(results, *embedding)
		accumulateUsage(&usageAcc, usage)
	}

	return &BatchResult{Embeddings: results, Usage: usagePointer(usageAcc, len(results))}, nil
}

func (c *HTTPEmbeddingClient) EmbedAudio(ctx context.Context, inputs []AudioInput) (*BatchResult, error) {
	return nil, errors.New("audio embedding not supported in HTTPEmbeddingClient")
}

func (c *HTTPEmbeddingClient) embedTexts(ctx context.Context, model string, inputs []TextInput) (*BatchResult, error) {
	results := make([]Embedding, 0, len(inputs))
	var usageAcc Usage

	for _, input := range inputs {
		text := strings.TrimSpace(input.Text)
		if text == "" {
			return nil, fmt.Errorf("text input %s empty", input.ID)
		}

		embedding, usage, err := c.invoke(ctx, model, []map[string]any{{"text": text}})
		if err != nil {
			return nil, err
		}
		embedding.ID = input.ID
		if embedding.Metadata == nil {
			embedding.Metadata = map[string]string{}
		}
		if input.Language != "" {
			embedding.Metadata["language"] = input.Language
		}
		for k, v := range input.Metadata {
			if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
				continue
			}
			embedding.Metadata[k] = v
		}
		results = append(results, *embedding)
		accumulateUsage(&usageAcc, usage)
	}

	return &BatchResult{Embeddings: results, Usage: usagePointer(usageAcc, len(results))}, nil
}

func (c *HTTPEmbeddingClient) EmbedImage(ctx context.Context, inputs []ImageInput) (*BatchResult, error) {
	if len(inputs) == 0 {
		return nil, ErrEmptyInput
	}

	model, err := c.cfg.ModelFor(ModalityImage)
	if err != nil {
		return nil, err
	}

	results := make([]Embedding, 0, len(inputs))
	var usageAcc Usage

	for _, input := range inputs {
		if len(input.Data) == 0 {
			return nil, fmt.Errorf("image input %s empty", input.ID)
		}

		mimeType := normalizeMime(strings.TrimSpace(input.MIMEType), "image/png")
		dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(input.Data))

		embedding, usage, err := c.invoke(ctx, model, []map[string]any{{"image": dataURI}})
		if err != nil {
			return nil, err
		}
		embedding.ID = input.ID
		if embedding.Metadata == nil {
			embedding.Metadata = map[string]string{}
		}
		embedding.Metadata["mime_type"] = mimeType
		for k, v := range input.Metadata {
			if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
				continue
			}
			embedding.Metadata[k] = v
		}
		results = append(results, *embedding)
		accumulateUsage(&usageAcc, usage)
	}

	return &BatchResult{Embeddings: results, Usage: usagePointer(usageAcc, len(results))}, nil
}

func (c *HTTPEmbeddingClient) EmbedVideo(ctx context.Context, inputs []VideoInput) (*BatchResult, error) {
	if len(inputs) == 0 {
		return nil, ErrEmptyInput
	}

	model, err := c.cfg.ModelFor(ModalityVideo)
	if err != nil {
		return nil, err
	}

	results := make([]Embedding, 0, len(inputs))
	var usageAcc Usage

	for _, input := range inputs {
		if len(input.Data) == 0 {
			return nil, fmt.Errorf("video input %s empty", input.ID)
		}

		mimeType := normalizeMime(strings.TrimSpace(input.MIMEType), "video/mp4")
		dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(input.Data))

		content := map[string]any{"video": dataURI}
		if input.FrameRate > 0 {
			content["frame_rate"] = input.FrameRate
		}

		embedding, usage, err := c.invoke(ctx, model, []map[string]any{content})
		if err != nil {
			return nil, err
		}
		embedding.ID = input.ID
		if embedding.Metadata == nil {
			embedding.Metadata = map[string]string{}
		}
		embedding.Metadata["mime_type"] = mimeType
		if input.FrameRate > 0 {
			embedding.Metadata["frame_rate"] = fmt.Sprintf("%.2f", input.FrameRate)
		}
		for k, v := range input.Metadata {
			if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
				continue
			}
			embedding.Metadata[k] = v
		}
		results = append(results, *embedding)
		accumulateUsage(&usageAcc, usage)
	}

	return &BatchResult{Embeddings: results, Usage: usagePointer(usageAcc, len(results))}, nil
}

func (c *HTTPEmbeddingClient) invoke(ctx context.Context, model string, contents []map[string]any) (*Embedding, *Usage, error) {
	if len(contents) == 0 {
		return nil, nil, ErrEmptyInput
	}

	endpoint := strings.TrimSpace(c.cfg.BaseURL)
	if endpoint == "" {
		return nil, nil, fmt.Errorf("embedding endpoint is empty")
	}

	reqBody := dashScopeRequest{
		Model: model,
		Input: dashScopeInput{Contents: contents},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("build embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("invoke embedding service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		failureBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		var apiErr dashScopeError
		if len(failureBytes) > 0 {
			_ = json.Unmarshal(failureBytes, &apiErr)
		}
		msg := strings.TrimSpace(apiErr.Message)
		if msg == "" {
			msg = strings.TrimSpace(string(failureBytes))
		}
		if msg == "" {
			msg = resp.Status
		}
		return nil, nil, fmt.Errorf("embedding request failed (status=%d, code=%s): %s", resp.StatusCode, apiErr.Code, msg)
	}

	var payload dashScopeResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if payload.Code != "" && payload.Message != "" {
		return nil, nil, fmt.Errorf("embedding service error (code=%s): %s", payload.Code, payload.Message)
	}
	if len(payload.Output.Embeddings) == 0 {
		return nil, nil, fmt.Errorf("embedding service returned empty result")
	}

	embedData := payload.Output.Embeddings[0]
	vector := make([]float32, len(embedData.Embedding))
	for i, val := range embedData.Embedding {
		vector[i] = float32(val)
	}

	metadata := map[string]string{}
	if embedData.Type != "" {
		metadata["type"] = embedData.Type
	}

	embedding := &Embedding{
		Vector:    vector,
		Model:     model,
		Dimension: len(vector),
		Metadata:  metadata,
	}

	usage := &Usage{
		PromptTokens: payload.Usage.InputTokens,
		TotalTokens:  payload.Usage.InputTokens,
		Latency:      time.Since(start),
	}

	return embedding, usage, nil
}

func accumulateUsage(acc *Usage, usage *Usage) {
	if usage == nil {
		return
	}
	acc.PromptTokens += usage.PromptTokens
	acc.TotalTokens += usage.TotalTokens
	acc.Latency += usage.Latency
}

func usagePointer(acc Usage, count int) *Usage {
	if count == 0 {
		return nil
	}
	return &Usage{
		PromptTokens: acc.PromptTokens,
		TotalTokens:  acc.TotalTokens,
		Latency:      acc.Latency,
	}
}

func normalizeMime(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func joinNonEmpty(sep string, parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}
