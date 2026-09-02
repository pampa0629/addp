package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	commoninference "github.com/addp/common/inference"
	secretcipher "github.com/addp/common/secretcipher"
	"github.com/addp/inference/internal/models"
	"github.com/addp/inference/internal/repository"
)

type Runtime struct {
	store         *repository.Store
	encryptionKey []byte
	client        *http.Client
}

var inferenceNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,127}$`)

func NewRuntime(store *repository.Store, encryptionKey []byte) *Runtime {
	return &Runtime{store: store, encryptionKey: append([]byte(nil), encryptionKey...), client: &http.Client{Timeout: 2 * time.Minute}}
}

type resolvedModel struct {
	profile    *models.ModelProfile
	deployment *models.ModelDeployment
	provider   *models.ProviderConnection
	credential string
}

type ProbeResponse struct {
	Reachable            bool   `json:"reachable"`
	ProviderConnectionID string `json:"provider_connection_id"`
	ModelDeploymentID    string `json:"model_deployment_id"`
	AdapterType          string `json:"adapter_type"`
	StatusCode           int    `json:"status_code"`
}

type DiscoveredModel struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by,omitempty"`
}

type ModelDiscoveryResponse struct {
	ProviderConnectionID string            `json:"provider_connection_id"`
	Models               []DiscoveredModel `json:"models"`
}

func (s *Runtime) DiscoverModels(ctx context.Context, actor Actor, providerID string) (*ModelDiscoveryResponse, error) {
	if err := validateActor(actor); err != nil {
		return nil, err
	}
	provider, err := s.store.GetProvider(ctx, strings.TrimSpace(providerID), false)
	if repository.IsNotFound(err) || (err == nil && !canManageProvider(actor, provider)) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if provider.AdapterType != AdapterOpenAICompatible {
		return nil, ErrUnsupported
	}
	endpoint, err := joinEndpoint(provider.Endpoint, "models")
	if err != nil {
		return nil, ErrInvalidRequest
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	if provider.CredentialCiphertext != "" {
		credential, decryptErr := secretcipher.Decrypt(provider.CredentialCiphertext, s.encryptionKey)
		if decryptErr != nil {
			return nil, fmt.Errorf("decrypt provider credential: %w", decryptErr)
		}
		request.Header.Set("Authorization", "Bearer "+credential)
	}
	response, err := s.client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ErrTimeout
		}
		return nil, fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, ErrUpstreamFailed
	}
	if response.StatusCode >= 500 {
		return nil, ErrUpstreamUnavailable
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, ErrUpstreamFailed
	}
	var upstream struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &upstream); err != nil {
		return nil, ErrUpstreamFailed
	}
	seen := make(map[string]bool, len(upstream.Data))
	models := make([]DiscoveredModel, 0, len(upstream.Data))
	for _, item := range upstream.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		models = append(models, DiscoveredModel{ID: id, OwnedBy: strings.TrimSpace(item.OwnedBy)})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return &ModelDiscoveryResponse{ProviderConnectionID: provider.ID, Models: models}, nil
}

func (s *Runtime) ResolveProfile(ctx context.Context, req commoninference.ResolveProfileRequest) (*commoninference.ResolveProfileResponse, error) {
	if req.SchemaVersion != commoninference.SchemaVersion || req.TenantID == 0 ||
		strings.TrimSpace(req.ModelProfileID) == "" || strings.TrimSpace(req.Operation) == "" || strings.TrimSpace(req.Modality) == "" {
		return nil, ErrInvalidRequest
	}
	resolved, err := s.resolve(ctx, req.TenantID, req.ModelProfileID, req.Operation, req.Modality)
	if err != nil {
		return nil, err
	}
	return &commoninference.ResolveProfileResponse{
		SchemaVersion: commoninference.SchemaVersion, ModelProfileID: resolved.profile.ID,
		ProfileVersion: int64(resolved.profile.Version), DeploymentID: resolved.deployment.ID,
		Dimension: resolved.deployment.Dimension,
	}, nil
}

func (s *Runtime) Chat(ctx context.Context, req commoninference.ChatRequest) (*commoninference.ChatResponse, error) {
	if req.SchemaVersion != commoninference.SchemaVersion || req.TenantID == 0 || len(req.Messages) == 0 {
		return nil, ErrInvalidRequest
	}
	upstreamMessages, err := normalizeChatMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	upstreamTools, err := normalizeChatTools(req.Tools)
	if err != nil {
		return nil, err
	}
	if len(req.Tools) > 0 && req.ResponseSchema != nil {
		return nil, fmt.Errorf("%w: tools and response_schema are mutually exclusive", ErrInvalidRequest)
	}
	if len(req.Tools) == 0 && req.ToolChoice != "" {
		return nil, fmt.Errorf("%w: tool_choice requires tools", ErrInvalidRequest)
	}
	if req.ToolChoice != "" && req.ToolChoice != "auto" && req.ToolChoice != "none" && req.ToolChoice != "required" {
		return nil, fmt.Errorf("%w: invalid tool_choice", ErrInvalidRequest)
	}
	resolved, err := s.resolve(ctx, req.TenantID, req.ModelProfileID, commoninference.OperationChat, commoninference.ModalityText)
	if err != nil {
		return nil, err
	}
	if resolved.provider.AdapterType != AdapterOpenAICompatible {
		return nil, ErrUnsupported
	}
	body := map[string]interface{}{"model": resolved.deployment.UpstreamModel, "messages": upstreamMessages, "stream": false}
	if len(upstreamTools) > 0 {
		body["tools"] = upstreamTools
		body["tool_choice"] = req.ToolChoice
		if req.ToolChoice == "" {
			body["tool_choice"] = "auto"
		}
	}
	if req.ResponseSchema != nil {
		responseFormat, schemaErr := normalizeResponseSchema(*req.ResponseSchema)
		if schemaErr != nil {
			return nil, schemaErr
		}
		body["response_format"] = responseFormat
	}
	switch resolved.deployment.ChatTemperatureMode {
	case ChatTemperatureModeConfigurable:
		if req.Temperature != nil {
			body["temperature"] = *req.Temperature
		}
	case ChatTemperatureModeDefaultOnly:
		// The upstream owns the only supported temperature value.
	default:
		return nil, ErrProfileUnavailable
	}
	if req.MaxOutputTokens > 0 {
		switch resolved.deployment.ChatMaxOutputTokensParameter {
		case ChatMaxOutputTokensParameterMaxTokens, ChatMaxOutputTokensParameterMaxCompletionTokens:
			body[resolved.deployment.ChatMaxOutputTokensParameter] = req.MaxOutputTokens
		default:
			return nil, ErrProfileUnavailable
		}
	}
	var upstream struct {
		Choices []struct {
			Message struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
			Total      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := s.invoke(ctx, resolved, "chat/completions", body, &upstream); err != nil {
		return nil, err
	}
	if len(upstream.Choices) == 0 {
		return nil, fmt.Errorf("%w: empty chat choices", ErrUpstreamFailed)
	}
	message, err := normalizeUpstreamChatMessage(upstream.Choices[0].Message.Role, upstream.Choices[0].Message.Content, upstream.Choices[0].Message.ToolCalls)
	if err != nil {
		return nil, err
	}
	return &commoninference.ChatResponse{SchemaVersion: commoninference.SchemaVersion, Message: message, Usage: commoninference.Usage{InputTokens: upstream.Usage.Prompt, OutputTokens: upstream.Usage.Completion, TotalTokens: upstream.Usage.Total}, DeploymentID: resolved.deployment.ID, ProfileVersion: int64(resolved.profile.Version)}, nil
}

func normalizeChatMessages(messages []commoninference.Message) ([]map[string]interface{}, error) {
	result := make([]map[string]interface{}, 0, len(messages))
	for _, message := range messages {
		role := strings.TrimSpace(message.Role)
		if role != "system" && role != "user" && role != "assistant" && role != "tool" {
			return nil, fmt.Errorf("%w: invalid chat role", ErrInvalidRequest)
		}
		if role == "tool" {
			if strings.TrimSpace(message.ToolCallID) == "" || len(message.ToolCalls) > 0 {
				return nil, fmt.Errorf("%w: invalid tool result message", ErrInvalidRequest)
			}
		} else if message.ToolCallID != "" {
			return nil, fmt.Errorf("%w: tool_call_id is only valid for tool messages", ErrInvalidRequest)
		}
		if role != "assistant" && len(message.ToolCalls) > 0 {
			return nil, fmt.Errorf("%w: tool_calls are only valid for assistant messages", ErrInvalidRequest)
		}
		if strings.TrimSpace(message.Content) == "" && !(role == "assistant" && len(message.ToolCalls) > 0) {
			return nil, fmt.Errorf("%w: chat message content is required", ErrInvalidRequest)
		}
		item := map[string]interface{}{"role": role}
		if message.Content != "" {
			item["content"] = message.Content
		}
		if message.ToolCallID != "" {
			item["tool_call_id"] = message.ToolCallID
		}
		if len(message.ToolCalls) > 0 {
			calls := make([]map[string]interface{}, 0, len(message.ToolCalls))
			seen := map[string]bool{}
			for _, call := range message.ToolCalls {
				if strings.TrimSpace(call.ID) == "" || !inferenceNamePattern.MatchString(call.Name) || seen[call.ID] || !validJSONObject(call.Arguments) {
					return nil, fmt.Errorf("%w: invalid assistant tool call", ErrInvalidRequest)
				}
				seen[call.ID] = true
				calls = append(calls, map[string]interface{}{"id": call.ID, "type": "function", "function": map[string]interface{}{"name": call.Name, "arguments": string(call.Arguments)}})
			}
			item["tool_calls"] = calls
		}
		result = append(result, item)
	}
	return result, nil
}

func normalizeChatTools(tools []commoninference.ToolDefinition) ([]map[string]interface{}, error) {
	result := make([]map[string]interface{}, 0, len(tools))
	seen := map[string]bool{}
	for _, tool := range tools {
		if !inferenceNamePattern.MatchString(tool.Name) || seen[tool.Name] || !validJSONObject(tool.Parameters) {
			return nil, fmt.Errorf("%w: invalid chat tool", ErrInvalidRequest)
		}
		seen[tool.Name] = true
		result = append(result, map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": tool.Name, "description": tool.Description, "parameters": tool.Parameters}})
	}
	return result, nil
}

func normalizeResponseSchema(value commoninference.ResponseSchema) (map[string]interface{}, error) {
	if !inferenceNamePattern.MatchString(value.Name) || !validJSONObject(value.Schema) {
		return nil, fmt.Errorf("%w: invalid response schema", ErrInvalidRequest)
	}
	return map[string]interface{}{"type": "json_schema", "json_schema": map[string]interface{}{"name": value.Name, "description": value.Description, "schema": value.Schema, "strict": value.Strict}}, nil
}

func validJSONObject(value json.RawMessage) bool {
	var decoded map[string]interface{}
	return len(value) > 0 && json.Unmarshal(value, &decoded) == nil && decoded != nil
}

func normalizeUpstreamChatMessage(role, content string, calls []struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}) (commoninference.Message, error) {
	if role != "assistant" {
		return commoninference.Message{}, fmt.Errorf("%w: invalid upstream assistant role", ErrUpstreamFailed)
	}
	message := commoninference.Message{Role: role, Content: content}
	for _, call := range calls {
		arguments := json.RawMessage(call.Function.Arguments)
		if call.Type != "function" || strings.TrimSpace(call.ID) == "" || !inferenceNamePattern.MatchString(call.Function.Name) || !validJSONObject(arguments) {
			return commoninference.Message{}, fmt.Errorf("%w: invalid upstream tool call", ErrUpstreamFailed)
		}
		message.ToolCalls = append(message.ToolCalls, commoninference.ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: arguments})
	}
	if strings.TrimSpace(message.Content) == "" && len(message.ToolCalls) == 0 {
		return commoninference.Message{}, fmt.Errorf("%w: empty assistant message", ErrUpstreamFailed)
	}
	return message, nil
}

func (s *Runtime) Embed(ctx context.Context, req commoninference.EmbeddingRequest) (*commoninference.EmbeddingResponse, error) {
	if req.SchemaVersion != commoninference.SchemaVersion || req.TenantID == 0 || len(req.Inputs) == 0 {
		return nil, ErrInvalidRequest
	}
	modalities := map[string]bool{}
	for _, input := range req.Inputs {
		modalities[input.Modality] = true
	}
	if len(modalities) != 1 {
		return nil, fmt.Errorf("%w: one modality per request", ErrInvalidRequest)
	}
	modality := ""
	for value := range modalities {
		modality = value
	}
	resolved, err := s.resolve(ctx, req.TenantID, req.ModelProfileID, commoninference.OperationEmbedding, modality)
	if err != nil {
		return nil, err
	}
	var vectors [][]float32
	var usage commoninference.Usage
	switch resolved.provider.AdapterType {
	case AdapterOpenAICompatible:
		if modality != commoninference.ModalityText {
			return nil, ErrUnsupported
		}
		inputs := make([]string, 0, len(req.Inputs))
		for _, input := range req.Inputs {
			if strings.TrimSpace(input.Text) == "" {
				return nil, ErrInvalidRequest
			}
			inputs = append(inputs, input.Text)
		}
		var upstream struct {
			Data []struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			} `json:"data"`
			Usage struct {
				Prompt int `json:"prompt_tokens"`
				Total  int `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := s.invoke(ctx, resolved, "embeddings", map[string]interface{}{"model": resolved.deployment.UpstreamModel, "input": inputs}, &upstream); err != nil {
			return nil, err
		}
		sort.Slice(upstream.Data, func(i, j int) bool { return upstream.Data[i].Index < upstream.Data[j].Index })
		for _, item := range upstream.Data {
			vectors = append(vectors, item.Embedding)
		}
		usage = commoninference.Usage{InputTokens: upstream.Usage.Prompt, TotalTokens: upstream.Usage.Total}
	case AdapterDashScopeMultimodal:
		contents := make([]map[string]interface{}, 0, len(req.Inputs))
		for _, input := range req.Inputs {
			switch modality {
			case commoninference.ModalityText:
				if strings.TrimSpace(input.Text) == "" {
					return nil, ErrInvalidRequest
				}
				contents = append(contents, map[string]interface{}{"text": input.Text})
			case commoninference.ModalityImage:
				if input.Data == "" || !validBase64(input.Data) {
					return nil, ErrInvalidRequest
				}
				mime := strings.TrimSpace(input.MIMEType)
				if mime == "" {
					mime = "image/png"
				}
				contents = append(contents, map[string]interface{}{"image": "data:" + mime + ";base64," + input.Data})
			default:
				return nil, ErrUnsupported
			}
		}
		var upstream struct {
			Output struct {
				Embeddings []struct {
					Index     int       `json:"index"`
					Embedding []float32 `json:"embedding"`
				} `json:"embeddings"`
			} `json:"output"`
			Usage struct {
				Input int `json:"input_tokens"`
				Image int `json:"image_tokens"`
			} `json:"usage"`
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if err := s.invokeAt(ctx, resolved, resolved.provider.Endpoint, map[string]interface{}{"model": resolved.deployment.UpstreamModel, "input": map[string]interface{}{"contents": contents}}, &upstream); err != nil {
			return nil, err
		}
		if upstream.Code != "" {
			return nil, fmt.Errorf("%w: %s", ErrUpstreamFailed, upstream.Code)
		}
		sort.Slice(upstream.Output.Embeddings, func(i, j int) bool { return upstream.Output.Embeddings[i].Index < upstream.Output.Embeddings[j].Index })
		for _, item := range upstream.Output.Embeddings {
			vectors = append(vectors, item.Embedding)
		}
		usage = commoninference.Usage{InputTokens: upstream.Usage.Input + upstream.Usage.Image, TotalTokens: upstream.Usage.Input + upstream.Usage.Image}
	default:
		return nil, ErrUnsupported
	}
	if len(vectors) != len(req.Inputs) || len(vectors) == 0 {
		return nil, fmt.Errorf("%w: invalid embedding count", ErrUpstreamFailed)
	}
	dimension := len(vectors[0])
	for _, vector := range vectors {
		if len(vector) != dimension {
			return nil, fmt.Errorf("%w: inconsistent embedding dimension", ErrUpstreamFailed)
		}
	}
	if resolved.deployment.Dimension > 0 && resolved.deployment.Dimension != dimension {
		return nil, fmt.Errorf("%w: deployment dimension mismatch", ErrUpstreamFailed)
	}
	return &commoninference.EmbeddingResponse{SchemaVersion: commoninference.SchemaVersion, Vectors: vectors, Dimension: dimension, Usage: usage, DeploymentID: resolved.deployment.ID, ProfileVersion: int64(resolved.profile.Version)}, nil
}

func (s *Runtime) Rerank(ctx context.Context, req commoninference.RerankRequest) (*commoninference.RerankResponse, error) {
	if req.SchemaVersion != commoninference.SchemaVersion || req.TenantID == 0 || strings.TrimSpace(req.Query) == "" || len(req.Documents) == 0 {
		return nil, ErrInvalidRequest
	}
	resolved, err := s.resolve(ctx, req.TenantID, req.ModelProfileID, commoninference.OperationRerank, commoninference.ModalityText)
	if err != nil {
		return nil, err
	}
	if resolved.provider.AdapterType != AdapterOpenAICompatible {
		return nil, ErrUnsupported
	}
	documents := make([]string, 0, len(req.Documents))
	for _, document := range req.Documents {
		documents = append(documents, document.Text)
	}
	var upstream struct {
		Results []struct {
			Index int     `json:"index"`
			Score float64 `json:"relevance_score"`
		} `json:"results"`
		Usage struct {
			Total int `json:"total_tokens"`
		} `json:"usage"`
	}
	body := map[string]interface{}{"model": resolved.deployment.UpstreamModel, "query": req.Query, "documents": documents}
	if req.TopN > 0 {
		body["top_n"] = req.TopN
	}
	if err := s.invoke(ctx, resolved, "rerank", body, &upstream); err != nil {
		return nil, err
	}
	results := make([]commoninference.RerankResult, 0, len(upstream.Results))
	for _, item := range upstream.Results {
		if item.Index < 0 || item.Index >= len(req.Documents) {
			return nil, fmt.Errorf("%w: invalid rerank index", ErrUpstreamFailed)
		}
		results = append(results, commoninference.RerankResult{DocumentID: req.Documents[item.Index].ID, Index: item.Index, Score: item.Score})
	}
	return &commoninference.RerankResponse{SchemaVersion: commoninference.SchemaVersion, Results: results, Usage: commoninference.Usage{TotalTokens: upstream.Usage.Total}, DeploymentID: resolved.deployment.ID, ProfileVersion: int64(resolved.profile.Version)}, nil
}

func (s *Runtime) Probe(ctx context.Context, actor Actor, deploymentID string) (*ProbeResponse, error) {
	if err := validateActor(actor); err != nil {
		return nil, err
	}
	deployment, err := s.store.GetDeployment(ctx, deploymentID)
	if repository.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	provider, err := s.store.GetProvider(ctx, deployment.ProviderConnectionID, false)
	if err != nil || !canManageProvider(actor, provider) {
		return nil, ErrNotFound
	}
	endpoint := provider.Endpoint
	if provider.AdapterType == AdapterOpenAICompatible {
		endpoint, err = joinEndpoint(provider.Endpoint, "models/"+url.PathEscape(deployment.UpstreamModel))
		if err != nil {
			return nil, ErrInvalidRequest
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	if provider.CredentialCiphertext != "" {
		credential, decryptErr := secretcipher.Decrypt(provider.CredentialCiphertext, s.encryptionKey)
		if decryptErr != nil {
			return nil, fmt.Errorf("decrypt provider credential: %w", decryptErr)
		}
		request.Header.Set("Authorization", "Bearer "+credential)
	}
	response, err := s.client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ErrTimeout
		}
		return nil, fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	if response.StatusCode >= 500 {
		return nil, ErrUpstreamUnavailable
	}
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return nil, ErrUpstreamFailed
	}
	return &ProbeResponse{Reachable: true, ProviderConnectionID: provider.ID, ModelDeploymentID: deployment.ID, AdapterType: provider.AdapterType, StatusCode: response.StatusCode}, nil
}

func (s *Runtime) resolve(ctx context.Context, tenantID uint, profileID, operation, modality string) (*resolvedModel, error) {
	profile, err := s.store.GetProfile(ctx, profileID)
	if repository.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if profile.Status != models.StatusActive {
		return nil, ErrProfileUnavailable
	}
	if profile.ScopeType == models.ScopeTenant && tenantValue(profile.TenantID) != tenantID {
		return nil, ErrNotFound
	}
	deployment, err := s.store.GetDeployment(ctx, profile.ModelDeploymentID)
	if err != nil {
		return nil, ErrProfileUnavailable
	}
	provider, err := s.store.GetProvider(ctx, deployment.ProviderConnectionID, false)
	if err != nil {
		return nil, ErrProfileUnavailable
	}
	if deployment.Status != models.StatusActive || provider.Status != models.StatusActive {
		return nil, ErrProfileUnavailable
	}
	allowed, err := providerAllowedForTenant(ctx, s.store, provider, tenantID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrForbidden
	}
	operations, err := decodeStrings(deployment.Operations)
	if err != nil || !contains(operations, operation) {
		return nil, ErrUnsupported
	}
	modalities, err := decodeStrings(deployment.Modalities)
	if err != nil || !contains(modalities, modality) {
		return nil, ErrUnsupported
	}
	credential := ""
	if provider.CredentialCiphertext != "" {
		credential, err = secretcipher.Decrypt(provider.CredentialCiphertext, s.encryptionKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt provider credential: %w", err)
		}
	}
	return &resolvedModel{profile: profile, deployment: deployment, provider: provider, credential: credential}, nil
}
func (s *Runtime) invoke(ctx context.Context, resolved *resolvedModel, path string, body, target interface{}) error {
	endpoint, err := joinEndpoint(resolved.provider.Endpoint, path)
	if err != nil {
		return err
	}
	return s.invokeAt(ctx, resolved, endpoint, body, target)
}
func (s *Runtime) invokeAt(ctx context.Context, resolved *resolvedModel, endpoint string, body, target interface{}) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if resolved.credential != "" {
		request.Header.Set("Authorization", "Bearer "+resolved.credential)
	}
	response, err := s.client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return ErrTimeout
		}
		return fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("%w: read response", ErrUpstreamFailed)
	}
	if response.StatusCode >= 500 {
		if response.StatusCode == http.StatusGatewayTimeout {
			return ErrTimeout
		}
		return fmt.Errorf("%w: HTTP %d", ErrUpstreamUnavailable, response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%w: HTTP %d", ErrUpstreamFailed, response.StatusCode)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("%w: decode response", ErrUpstreamFailed)
	}
	return nil
}
func joinEndpoint(base, path string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(base, "/") + "/")
	if err != nil {
		return "", err
	}
	return parsed.ResolveReference(&url.URL{Path: path}).String(), nil
}
func validBase64(value string) bool {
	_, err := base64.StdEncoding.DecodeString(value)
	return err == nil
}
