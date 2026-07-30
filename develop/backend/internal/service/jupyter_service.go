package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/utils"
)

// JupyterService resolves the task-bound Script Engine through System and
// calls the controlled runtime endpoint with a tenant service token.
type JupyterService struct {
	systemClient  *commonClient.SystemServiceClient
	serviceTokens commonClient.ServiceTokenProvider
	httpClient    *http.Client
}

func NewJupyterService(
	systemClient *commonClient.SystemServiceClient,
	serviceTokens commonClient.ServiceTokenProvider,
) *JupyterService {
	return &JupyterService{
		systemClient: systemClient, serviceTokens: serviceTokens,
		httpClient: &http.Client{},
	}
}

type KernelInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Language    string `json:"language"`
}

type ListKernelsResponse struct {
	Status  string       `json:"status"`
	Kernels []KernelInfo `json:"kernels"`
}

type JupyterRuntimeExecutionRequest struct {
	TenantID     uint                   `json:"tenant_id"`
	NotebookPath string                 `json:"notebook_path"`
	Parameters   map[string]interface{} `json:"parameters"`
	Kernel       string                 `json:"kernel"`
}

type JupyterRuntimeExecutionResponse struct {
	Status               string                   `json:"status"`
	Message              string                   `json:"message"`
	ExecutionTimeSeconds float64                  `json:"execution_time_seconds"`
	OutputPath           string                   `json:"output_path"`
	CellCount            int                      `json:"cell_count"`
	ExecutionCount       int                      `json:"execution_count"`
	OutputCount          int                      `json:"output_count"`
	Outputs              []map[string]interface{} `json:"outputs"`
	ErrorMessage         string                   `json:"error_message"`
}

func (s *JupyterService) ListNotebookEngines(ctx context.Context, tenantID uint) ([]commonModels.EngineRuntimeDescriptor, error) {
	if s == nil || s.systemClient == nil {
		return nil, fmt.Errorf("System service client is required")
	}
	descriptors, err := s.systemClient.WithTenantID(tenantID).ListEngineRuntimeDescriptors(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]commonModels.EngineRuntimeDescriptor, 0, len(descriptors))
	for index := range descriptors {
		if validateNotebookEngineDescriptor(&descriptors[index]) == nil {
			result = append(result, descriptors[index])
		}
	}
	return result, nil
}

func (s *JupyterService) GetNotebookEngine(
	ctx context.Context,
	tenantID uint,
	engineID uint,
) (*commonModels.EngineRuntimeDescriptor, error) {
	if engineID == 0 {
		return nil, fmt.Errorf("notebook engine_id must be a positive integer")
	}
	if s == nil || s.systemClient == nil {
		return nil, fmt.Errorf("System service client is required")
	}
	descriptor, err := s.systemClient.WithTenantID(tenantID).GetEngineRuntimeDescriptor(ctx, engineID)
	if err != nil {
		return nil, fmt.Errorf("get notebook engine %d: %w", engineID, err)
	}
	if err := validateNotebookEngineDescriptor(descriptor); err != nil {
		return nil, err
	}
	return descriptor, nil
}

func (s *JupyterService) ListKernels(ctx context.Context, tenantID uint, engineID uint) ([]KernelInfo, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	endpoint, err := s.openNotebookEndpoint(requestCtx, tenantID, engineID)
	if err != nil {
		return nil, err
	}
	responseBody, err := s.doRuntimeRequest(requestCtx, tenantID, http.MethodGet, endpoint+"/api/kernels", nil)
	if err != nil {
		return nil, err
	}
	var result ListKernelsResponse
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return nil, fmt.Errorf("decode kernel response: %w", err)
	}
	if result.Status != "success" {
		return nil, fmt.Errorf("runtime rejected kernel discovery")
	}
	return result.Kernels, nil
}

func (s *JupyterService) ValidateKernel(ctx context.Context, tenantID uint, engineID uint, kernelName string) error {
	kernels, err := s.ListKernels(ctx, tenantID, engineID)
	if err != nil {
		return err
	}
	for _, kernel := range kernels {
		if kernel.Name == kernelName {
			return nil
		}
	}
	return fmt.Errorf("kernel %q is not available on notebook engine %d", kernelName, engineID)
}

func (s *JupyterService) ExecuteNotebook(
	ctx context.Context,
	tenantID uint,
	engineID uint,
	request JupyterRuntimeExecutionRequest,
	timeout time.Duration,
) (*JupyterRuntimeExecutionResponse, error) {
	endpoint, err := s.openNotebookEndpoint(ctx, tenantID, engineID)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode notebook execution request: %w", err)
	}
	requestCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		requestCtx, cancel = context.WithTimeout(ctx, timeout+time.Minute)
		defer cancel()
	}
	responseBody, err := s.doRuntimeRequest(
		requestCtx, tenantID, http.MethodPost, endpoint+"/api/execute", bytes.NewReader(payload),
	)
	if err != nil {
		return nil, err
	}
	var result JupyterRuntimeExecutionResponse
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return nil, fmt.Errorf("decode notebook execution response: %w", err)
	}
	if result.Status == "error" {
		return nil, fmt.Errorf("notebook execution failed: %s", result.ErrorMessage)
	}
	return &result, nil
}

func (s *JupyterService) openNotebookEndpoint(ctx context.Context, tenantID uint, engineID uint) (string, error) {
	descriptor, err := s.GetNotebookEngine(ctx, tenantID, engineID)
	if err != nil {
		return "", err
	}
	session, err := dbbridge.OpenScriptSession(ctx, descriptor.AsEngine(), plugin.ScriptSessionRequest{
		Mode: "notebook", Language: "python",
	})
	if err != nil {
		return "", fmt.Errorf("open notebook session for engine %d: %w", engineID, err)
	}
	endpoint := strings.TrimRight(strings.TrimSpace(session.Endpoint), "/")
	if endpoint == "" {
		return "", fmt.Errorf("notebook engine %d returned an empty runtime endpoint", engineID)
	}
	return endpoint, nil
}

func (s *JupyterService) doRuntimeRequest(
	ctx context.Context,
	tenantID uint,
	method string,
	url string,
	body io.Reader,
) ([]byte, error) {
	if s == nil || s.serviceTokens == nil {
		return nil, fmt.Errorf("Develop service token provider is required")
	}
	token, err := s.serviceTokens.Token(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get Develop service token: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("create notebook runtime request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call notebook runtime: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read notebook runtime response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("notebook runtime returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return responseBody, nil
}

func validateNotebookEngineDescriptor(descriptor *commonModels.EngineRuntimeDescriptor) error {
	if descriptor == nil {
		return fmt.Errorf("notebook engine descriptor is required")
	}
	if descriptor.LifecycleState != commonModels.EngineLifecycleActive {
		return fmt.Errorf("notebook engine %d is not active", descriptor.ID)
	}
	capabilities, err := utils.ParseCapabilities(descriptor.Capabilities)
	if err != nil {
		return fmt.Errorf("notebook engine %d capabilities are invalid: %w", descriptor.ID, err)
	}
	if capabilities == nil || capabilities.Compute == nil || capabilities.Compute.Script == nil || !capabilities.Compute.Script.Supported {
		return fmt.Errorf("engine %d does not support script execution", descriptor.ID)
	}
	if capabilities.EngineType != descriptor.EngineType {
		return fmt.Errorf("notebook engine %d capabilities engine_type does not match", descriptor.ID)
	}
	if descriptor.RuntimeEndpoint == nil || strings.TrimSpace(descriptor.RuntimeEndpoint.Host) == "" || descriptor.RuntimeEndpoint.Port <= 0 {
		return fmt.Errorf("notebook engine %d has no runtime endpoint", descriptor.ID)
	}
	for _, mode := range capabilities.Compute.Script.Modes {
		if mode == "notebook" {
			return nil
		}
	}
	return fmt.Errorf("engine %d does not support notebook mode", descriptor.ID)
}
