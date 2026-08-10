package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	engineplugin "github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/inference_runtime"
	engineselection "github.com/addp/common/engine/selection"
	commoninference "github.com/addp/common/inference"
	commonmodels "github.com/addp/common/models"
)

var (
	ErrInferenceRuntimeNotFound  = errors.New("active inference runtime not found")
	ErrInferenceRuntimeAmbiguous = errors.New("multiple active inference runtimes found")
)

type InferenceClient struct {
	system      *SystemServiceClient
	tokenSource ServiceTokenProvider
}

func NewInferenceClient(systemURL string, tokenSource ServiceTokenSource, httpClient *http.Client) (*InferenceClient, error) {
	systemURL = strings.TrimRight(strings.TrimSpace(systemURL), "/")
	parsed, err := url.Parse(systemURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("inference client requires an absolute System HTTP(S) URL")
	}
	if tokenSource == nil {
		return nil, errors.New("inference client requires a service token source")
	}
	return &InferenceClient{
		system:      NewSystemServiceClient(systemURL, tokenSource, httpClient),
		tokenSource: tokenSource,
	}, nil
}

func (c *InferenceClient) Chat(ctx context.Context, req commoninference.ChatRequest) (*commoninference.ChatResponse, error) {
	var response *commoninference.ChatResponse
	err := c.call(ctx, req.TenantID, func(provider engineplugin.InferenceRuntimeProvider, connInfo engineplugin.ConnectionInfo, token string) error {
		req.CallerToken = token
		var err error
		response, err = provider.Chat(ctx, connInfo, req)
		return err
	})
	return response, err
}

func (c *InferenceClient) ResolveProfile(ctx context.Context, req commoninference.ResolveProfileRequest) (*commoninference.ResolveProfileResponse, error) {
	var response *commoninference.ResolveProfileResponse
	err := c.call(ctx, req.TenantID, func(provider engineplugin.InferenceRuntimeProvider, connInfo engineplugin.ConnectionInfo, token string) error {
		req.CallerToken = token
		var err error
		response, err = provider.ResolveProfile(ctx, connInfo, req)
		return err
	})
	return response, err
}

func (c *InferenceClient) Embed(ctx context.Context, req commoninference.EmbeddingRequest) (*commoninference.EmbeddingResponse, error) {
	var response *commoninference.EmbeddingResponse
	err := c.call(ctx, req.TenantID, func(provider engineplugin.InferenceRuntimeProvider, connInfo engineplugin.ConnectionInfo, token string) error {
		req.CallerToken = token
		var err error
		response, err = provider.Embed(ctx, connInfo, req)
		return err
	})
	return response, err
}

func (c *InferenceClient) Rerank(ctx context.Context, req commoninference.RerankRequest) (*commoninference.RerankResponse, error) {
	var response *commoninference.RerankResponse
	err := c.call(ctx, req.TenantID, func(provider engineplugin.InferenceRuntimeProvider, connInfo engineplugin.ConnectionInfo, token string) error {
		req.CallerToken = token
		var err error
		response, err = provider.Rerank(ctx, connInfo, req)
		return err
	})
	return response, err
}

func (c *InferenceClient) call(
	ctx context.Context,
	tenantID uint,
	invoke func(engineplugin.InferenceRuntimeProvider, engineplugin.ConnectionInfo, string) error,
) error {
	if tenantID == 0 {
		return errors.New("inference request requires tenant ID")
	}
	provider, connInfo, err := c.resolveRuntime(ctx, tenantID)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 2; attempt++ {
		token, err := c.tokenSource.Token(ctx, tenantID)
		if err != nil {
			return err
		}
		err = invoke(provider, connInfo, token)
		if err == nil {
			return nil
		}
		var runtimeErr *engineplugin.RuntimeHTTPError
		if errors.As(err, &runtimeErr) && runtimeErr.StatusCode == http.StatusUnauthorized && attempt == 0 {
			if invalidator, ok := c.tokenSource.(ServiceTokenInvalidator); ok {
				invalidator.InvalidateToken(tenantID, token)
				continue
			}
		}
		return inferenceRuntimeError(err)
	}
	return errors.New("inference request retry exhausted")
}

func (c *InferenceClient) resolveRuntime(
	ctx context.Context,
	tenantID uint,
) (engineplugin.InferenceRuntimeProvider, engineplugin.ConnectionInfo, error) {
	descriptors, err := c.system.WithTenantID(tenantID).ListEngineRuntimeDescriptors(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("discover inference runtime: %w", err)
	}
	candidates := make([]commonmodels.EngineRuntimeDescriptor, 0, 1)
	for index := range descriptors {
		descriptor := &descriptors[index]
		engine := descriptor.AsEngine()
		capabilities, capabilityErr := engineselection.ParseCapabilities(engine.Capabilities)
		if descriptor.EngineType != "inference_runtime" || !descriptor.IsBuiltin ||
			descriptor.LifecycleState != commonmodels.EngineLifecycleActive || capabilityErr != nil ||
			capabilities == nil || capabilities.EngineType != "inference_runtime" ||
			capabilities.EngineFamily != "inference" ||
			!engineselection.SupportsComputeEntrypoint(engine, "inference") ||
			capabilities.Compute == nil || capabilities.Compute.Inference == nil ||
			capabilities.Compute.Inference.RuntimeAPI != commoninference.SchemaVersion {
			continue
		}
		candidates = append(candidates, *descriptor)
	}
	if len(candidates) == 0 {
		return nil, nil, ErrInferenceRuntimeNotFound
	}
	if len(candidates) != 1 {
		return nil, nil, fmt.Errorf("%w: %d candidates", ErrInferenceRuntimeAmbiguous, len(candidates))
	}
	descriptor := &candidates[0]
	engine := descriptor.AsEngine()
	if descriptor.RuntimeEndpoint == nil {
		return nil, nil, errors.New("inference runtime descriptor has no endpoint")
	}
	registered, err := engineplugin.Get(descriptor.EngineType)
	if err != nil {
		return nil, nil, fmt.Errorf("load inference runtime provider: %w", err)
	}
	provider, ok := registered.(engineplugin.InferenceRuntimeProvider)
	if !ok {
		return nil, nil, errors.New("inference runtime plugin does not implement InferenceRuntimeProvider")
	}
	engineConnectionInfo := engine.ConnectionInfo
	pluginConnectionInfo := engineplugin.ConnectionInfo{}
	for key, value := range engineConnectionInfo {
		pluginConnectionInfo[key] = value
	}
	return provider, pluginConnectionInfo, nil
}

func inferenceRuntimeError(err error) error {
	var runtimeErr *engineplugin.RuntimeHTTPError
	if !errors.As(err, &runtimeErr) {
		return err
	}
	var failure commoninference.ErrorResponse
	_ = json.Unmarshal(runtimeErr.Body, &failure)
	if failure.ErrorCode == "" {
		failure.ErrorCode = "inference_upstream_failed"
	}
	return fmt.Errorf("%s: %s", failure.ErrorCode, failure.Error)
}
