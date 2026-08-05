package inference_runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	commoninference "github.com/addp/common/inference"
)

type Plugin struct{}

func init() { plugin.Register(&Plugin{}) }

func (p *Plugin) Type() string              { return "inference_runtime" }
func (p *Plugin) DisplayName() string       { return "ADDP AI Inference Runtime" }
func (p *Plugin) EngineOrigin() string      { return "extension" }
func (p *Plugin) DefaultPort() int          { return 8191 }
func (p *Plugin) RequiredFields() []string  { return []string{"host", "port"} }
func (p *Plugin) SensitiveFields() []string { return nil }
func (p *Plugin) ConnectionIdentityFields() []string {
	return []string{"protocol", "host", "port"}
}
func (p *Plugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewInferenceCapabilities(p.Type(), []string{"chat", "embedding", "rerank"}, []string{"text", "image"}, false)
}
func (p *Plugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}
func (p *Plugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	baseURL, err := plugin.RuntimeBaseURL(connInfo)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("connect to inference runtime: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("inference runtime health check returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func (p *Plugin) Chat(ctx context.Context, connInfo plugin.ConnectionInfo, req commoninference.ChatRequest) (*commoninference.ChatResponse, error) {
	var response commoninference.ChatResponse
	if err := post(ctx, connInfo, "/api/v1/inference/internal/chat", req.CallerToken, req, &response); err != nil {
		return nil, err
	}
	return &response, nil
}
func (p *Plugin) Embed(ctx context.Context, connInfo plugin.ConnectionInfo, req commoninference.EmbeddingRequest) (*commoninference.EmbeddingResponse, error) {
	var response commoninference.EmbeddingResponse
	if err := post(ctx, connInfo, "/api/v1/inference/internal/embeddings", req.CallerToken, req, &response); err != nil {
		return nil, err
	}
	return &response, nil
}
func (p *Plugin) Rerank(ctx context.Context, connInfo plugin.ConnectionInfo, req commoninference.RerankRequest) (*commoninference.RerankResponse, error) {
	var response commoninference.RerankResponse
	if err := post(ctx, connInfo, "/api/v1/inference/internal/rerank", req.CallerToken, req, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func post(ctx context.Context, connInfo plugin.ConnectionInfo, path, token string, payload, target interface{}) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("inference runtime requires caller token")
	}
	baseURL, err := plugin.RuntimeBaseURL(connInfo)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	encoded, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("inference runtime returned HTTP %d: %s", resp.StatusCode, string(encoded))
	}
	return json.Unmarshal(encoded, target)
}
