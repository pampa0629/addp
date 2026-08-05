package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	commoninference "github.com/addp/common/inference"
)

type InferenceClient struct {
	baseURL     string
	httpClient  *http.Client
	tokenSource ServiceTokenProvider
}

func NewInferenceClient(baseURL string, tokenSource ServiceTokenProvider, httpClient *http.Client) (*InferenceClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || tokenSource == nil {
		return nil, errors.New("inference client requires base URL and service token provider")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 2 * time.Minute}
	}
	return &InferenceClient{baseURL: baseURL, httpClient: httpClient, tokenSource: tokenSource}, nil
}

func (c *InferenceClient) Chat(ctx context.Context, req commoninference.ChatRequest) (*commoninference.ChatResponse, error) {
	var response commoninference.ChatResponse
	if err := c.post(ctx, "/api/v1/inference/internal/chat", req.TenantID, req, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *InferenceClient) ResolveProfile(ctx context.Context, req commoninference.ResolveProfileRequest) (*commoninference.ResolveProfileResponse, error) {
	var response commoninference.ResolveProfileResponse
	if err := c.post(ctx, "/api/v1/inference/internal/profiles/resolve", req.TenantID, req, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *InferenceClient) Embed(ctx context.Context, req commoninference.EmbeddingRequest) (*commoninference.EmbeddingResponse, error) {
	var response commoninference.EmbeddingResponse
	if err := c.post(ctx, "/api/v1/inference/internal/embeddings", req.TenantID, req, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *InferenceClient) Rerank(ctx context.Context, req commoninference.RerankRequest) (*commoninference.RerankResponse, error) {
	var response commoninference.RerankResponse
	if err := c.post(ctx, "/api/v1/inference/internal/rerank", req.TenantID, req, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *InferenceClient) post(ctx context.Context, path string, tenantID uint, payload, target interface{}) error {
	if tenantID == 0 {
		return errors.New("inference request requires tenant ID")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 2; attempt++ {
		token, err := c.tokenSource.Token(ctx, tenantID)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		encoded, readErr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			if invalidator, ok := c.tokenSource.(ServiceTokenInvalidator); ok {
				invalidator.InvalidateToken(tenantID, token)
				continue
			}
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			var failure commoninference.ErrorResponse
			_ = json.Unmarshal(encoded, &failure)
			if failure.ErrorCode == "" {
				failure.ErrorCode = "inference_upstream_failed"
			}
			return fmt.Errorf("%s: %s", failure.ErrorCode, failure.Error)
		}
		if err := json.Unmarshal(encoded, target); err != nil {
			return fmt.Errorf("decode inference response: %w", err)
		}
		return nil
	}
	return errors.New("inference request retry exhausted")
}
