package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
	"github.com/addp/orchestrator/internal/models"
)

var ErrTaskProviderUnavailable = errors.New("task provider is currently unavailable")

// TaskProviderResolver 在每次使用边界从 System 动态解析模块声明和当前 Backend。
type TaskProviderResolver struct {
	systemClient          *commonClient.SystemServiceClient
	httpClient            *http.Client
	loadProvider          func(context.Context, string) (*commonModels.TaskProvider, error)
	listProviders         func(context.Context) ([]*commonModels.TaskProvider, error)
	loadExecutionContract func(context.Context, *commonModels.TaskProvider, string, uint, uint) (*taskprovider.ExecutionContract, error)
}

// NewTaskProviderResolver 创建任务提供者动态解析器。
func NewTaskProviderResolver(systemClient *commonClient.SystemServiceClient) *TaskProviderResolver {
	return &TaskProviderResolver{
		systemClient: systemClient,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

// GetProvider always resolves the current module Backend through System.
func (r *TaskProviderResolver) GetProvider(ctx context.Context, moduleName string) (*commonModels.TaskProvider, error) {
	moduleName = strings.TrimSpace(moduleName)
	var provider *commonModels.TaskProvider
	var err error
	if r.loadProvider != nil {
		provider, err = r.loadProvider(ctx, moduleName)
	} else {
		provider, err = r.systemClient.GetTaskProvider(ctx, moduleName)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get task provider %s: %w", moduleName, err)
	}
	if provider == nil || !provider.Available || strings.TrimSpace(provider.BaseURL) == "" {
		return nil, fmt.Errorf("%w: %s", ErrTaskProviderUnavailable, moduleName)
	}
	return provider, nil
}

// ListAllProviders 列出所有声明了 TaskProvider 角色的模块，包括当前不可用模块。
func (r *TaskProviderResolver) ListAllProviders(ctx context.Context) ([]*commonModels.TaskProvider, error) {
	if r.listProviders != nil {
		return r.listProviders(ctx)
	}
	providers, err := r.systemClient.ListTaskProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list task providers: %w", err)
	}
	return providers, nil
}

// ValidateStepTaskReferences 校验编排步骤引用的 provider/task_type 已由 TaskProvider capabilities 声明。
func (r *TaskProviderResolver) ValidateStepTaskReferences(ctx context.Context, tenantID uint, steps models.Steps) error {
	capabilityCache := map[string]*taskprovider.TaskCapability{}
	contractCache := map[string]*taskprovider.ExecutionContract{}
	contractsByStepID := map[string]*taskprovider.ExecutionContract{}
	for i, step := range steps {
		providerName := strings.TrimSpace(step.Provider)
		taskType := strings.TrimSpace(step.TaskType)
		if providerName == "" || taskType == "" {
			return &StepTaskValidationError{Code: StepTaskMissingReference, StepIndex: i, Provider: providerName, TaskType: taskType}
		}

		key := providerName + "\x00" + taskType
		taskTypeCapability, exists := capabilityCache[key]
		if !exists {
			provider, err := r.GetProvider(ctx, providerName)
			if err != nil {
				return &StepTaskValidationError{Code: StepTaskProviderUnavailable, StepIndex: i, Provider: providerName, TaskType: taskType, Cause: err}
			}

			taskTypeCapability, err = providerTaskCapability(provider, taskType)
			if err != nil {
				return &StepTaskValidationError{Code: StepTaskCapabilitiesInvalid, StepIndex: i, Provider: providerName, TaskType: taskType, Cause: err}
			}
			capabilityCache[key] = taskTypeCapability
		}
		if taskTypeCapability == nil {
			return &StepTaskValidationError{Code: StepTaskTypeUndeclared, StepIndex: i, Provider: providerName, TaskType: taskType}
		}
		if taskTypeCapability.Deprecated {
			return &StepTaskValidationError{Code: StepTaskTypeDeprecated, StepIndex: i, Provider: providerName, TaskType: taskType}
		}
		contractKey := key + "\x00" + strconv.FormatUint(uint64(step.TaskID), 10)
		contract, exists := contractCache[contractKey]
		if !exists {
			provider, err := r.GetProvider(ctx, providerName)
			if err != nil {
				return &StepTaskValidationError{Code: StepTaskProviderUnavailable, StepIndex: i, Provider: providerName, TaskType: taskType, Cause: err}
			}
			contract, err = r.GetTaskExecutionContract(ctx, provider, taskType, step.TaskID, tenantID)
			if err != nil {
				return &StepTaskValidationError{Code: StepTaskProviderUnavailable, StepIndex: i, Provider: providerName, TaskType: taskType, Cause: err}
			}
			contractCache[contractKey] = contract
		}
		if err := validateStepParametersByExecutionContract(step, contract, true); err != nil {
			return &StepTaskValidationError{Code: StepTaskParametersInvalid, StepIndex: i, Provider: providerName, TaskType: taskType, Cause: err}
		}
		contractsByStepID[step.ID] = contract
	}
	for i, step := range steps {
		if err := validateDeclaredOutputReferences(step, contractsByStepID); err != nil {
			return &StepTaskValidationError{
				Code: StepTaskParametersInvalid, StepIndex: i,
				Provider: step.Provider, TaskType: step.TaskType, Cause: err,
			}
		}
	}
	return nil
}

type executionTemplateReference struct {
	ParameterPath []string
	StepID        string
	OutputPath    []string
}

type OutputBindingValidationCode string

const (
	OutputBindingInvalidFormat OutputBindingValidationCode = "invalid_format"
	OutputBindingUnknownStep   OutputBindingValidationCode = "unknown_step"
	OutputBindingUndeclared    OutputBindingValidationCode = "undeclared_output"
	OutputBindingUnknownTarget OutputBindingValidationCode = "unknown_target"
	OutputBindingTypeMismatch  OutputBindingValidationCode = "type_mismatch"
)

type OutputBindingValidationError struct {
	Code          OutputBindingValidationCode
	ParameterPath string
	StepID        string
	OutputPath    string
	SourceType    string
	TargetType    string
}

func (e *OutputBindingValidationError) Error() string {
	switch e.Code {
	case OutputBindingInvalidFormat:
		return fmt.Sprintf("parameters.%s must reference a declared output as {{step_id.outputs.path}}", e.ParameterPath)
	case OutputBindingUnknownStep:
		return fmt.Sprintf("parameters.%s references unknown step %q", e.ParameterPath, e.StepID)
	case OutputBindingUndeclared:
		return fmt.Sprintf("parameters.%s references undeclared output %s.outputs.%s", e.ParameterPath, e.StepID, e.OutputPath)
	case OutputBindingUnknownTarget:
		return fmt.Sprintf("parameters.%s is not declared by target input contract", e.ParameterPath)
	case OutputBindingTypeMismatch:
		return fmt.Sprintf("parameters.%s cannot bind output type %q to input type %q", e.ParameterPath, e.SourceType, e.TargetType)
	default:
		return "output binding is invalid"
	}
}

func validateDeclaredOutputReferences(step models.Step, contracts map[string]*taskprovider.ExecutionContract) error {
	targetContract := contracts[step.ID]
	if targetContract == nil {
		return fmt.Errorf("target execution_contract is required")
	}
	references, err := collectExecutionTemplateReferences(step.Parameters)
	if err != nil {
		return err
	}
	for _, reference := range references {
		parameterPath := strings.Join(reference.ParameterPath, ".")
		sourceContract := contracts[reference.StepID]
		if sourceContract == nil {
			return &OutputBindingValidationError{Code: OutputBindingUnknownStep, ParameterPath: parameterPath, StepID: reference.StepID}
		}
		sourceSchema, ok := executionSchemaAtPath(sourceContract.OutputSchema, reference.OutputPath)
		if !ok {
			return &OutputBindingValidationError{
				Code: OutputBindingUndeclared, ParameterPath: parameterPath,
				StepID: reference.StepID, OutputPath: strings.Join(reference.OutputPath, "."),
			}
		}
		targetSchema, ok := executionSchemaAtPath(targetContract.InputSchema, reference.ParameterPath)
		if !ok {
			return &OutputBindingValidationError{Code: OutputBindingUnknownTarget, ParameterPath: parameterPath}
		}
		sourceType, _ := sourceSchema["type"].(string)
		targetType, _ := targetSchema["type"].(string)
		if sourceType == "" || targetType == "" || sourceType != targetType {
			return &OutputBindingValidationError{
				Code: OutputBindingTypeMismatch, ParameterPath: parameterPath,
				SourceType: sourceType, TargetType: targetType,
			}
		}
	}
	return nil
}

func collectExecutionTemplateReferences(parameters map[string]interface{}) ([]executionTemplateReference, error) {
	result := make([]executionTemplateReference, 0)
	var walk func(interface{}, []string) error
	walk = func(value interface{}, parameterPath []string) error {
		switch typed := value.(type) {
		case string:
			trimmed := strings.TrimSpace(typed)
			if !strings.HasPrefix(trimmed, "{{") || !strings.HasSuffix(trimmed, "}}") {
				return nil
			}
			parts := splitPath(strings.TrimSpace(trimmed[2 : len(trimmed)-2]))
			if len(parts) < 3 || parts[1] != "outputs" {
				return &OutputBindingValidationError{Code: OutputBindingInvalidFormat, ParameterPath: strings.Join(parameterPath, ".")}
			}
			result = append(result, executionTemplateReference{
				ParameterPath: append([]string(nil), parameterPath...),
				StepID:        parts[0],
				OutputPath:    append([]string(nil), parts[2:]...),
			})
		case map[string]interface{}:
			for name, nested := range typed {
				if err := walk(nested, append(parameterPath, name)); err != nil {
					return err
				}
			}
		case []interface{}:
			for index, nested := range typed {
				if err := walk(nested, append(parameterPath, strconv.Itoa(index))); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(parameters, nil); err != nil {
		return nil, err
	}
	return result, nil
}

func executionSchemaAtPath(schema map[string]interface{}, path []string) (map[string]interface{}, bool) {
	current := schema
	for _, name := range path {
		properties, ok := current["properties"].(map[string]interface{})
		if !ok {
			return nil, false
		}
		next, ok := properties[name].(map[string]interface{})
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func (r *TaskProviderResolver) GetTaskExecutionContract(
	ctx context.Context,
	provider *commonModels.TaskProvider,
	taskType string,
	taskID uint,
	tenantID uint,
) (*taskprovider.ExecutionContract, error) {
	if r.loadExecutionContract != nil {
		return r.loadExecutionContract(ctx, provider, taskType, taskID, tenantID)
	}
	payload, err := r.getTaskDetailPayload(ctx, provider, taskType, taskID, tenantID)
	if err != nil {
		return nil, err
	}
	contract, err := taskprovider.ParseExecutionContract(payload["execution_contract"])
	if err != nil {
		return nil, fmt.Errorf("task detail execution_contract invalid: %w", err)
	}
	return contract, nil
}

func (r *TaskProviderResolver) GetTaskDetail(ctx context.Context, moduleName, taskType string, taskID, tenantID uint) (map[string]interface{}, error) {
	provider, err := r.GetProvider(ctx, strings.TrimSpace(moduleName))
	if err != nil {
		return nil, err
	}
	capability, err := providerTaskCapability(provider, strings.TrimSpace(taskType))
	if err != nil {
		return nil, fmt.Errorf("provider %q capabilities invalid: %w", moduleName, err)
	}
	if capability == nil || capability.Deprecated {
		return nil, fmt.Errorf("task_type %q is not available from provider %q", taskType, moduleName)
	}
	payload, err := r.getTaskDetailPayload(ctx, provider, taskType, taskID, tenantID)
	if err != nil {
		return nil, err
	}
	if _, err := taskprovider.ParseExecutionContract(payload["execution_contract"]); err != nil {
		return nil, fmt.Errorf("task detail execution_contract invalid: %w", err)
	}
	return payload, nil
}

func (r *TaskProviderResolver) getTaskDetailPayload(
	ctx context.Context,
	provider *commonModels.TaskProvider,
	taskType string,
	taskID uint,
	tenantID uint,
) (map[string]interface{}, error) {
	if provider == nil || strings.TrimSpace(provider.BaseURL) == "" || strings.TrimSpace(provider.TaskDetailEndpoint) == "" {
		return nil, fmt.Errorf("task provider detail endpoint is unavailable")
	}
	if taskID == 0 || tenantID == 0 {
		return nil, fmt.Errorf("task_id and tenant_id are required")
	}
	endpoint := replaceTaskProviderEndpoint(provider.TaskDetailEndpoint, taskType, strconv.FormatUint(uint64(taskID), 10), "")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.BaseURL+endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create task detail request: %w", err)
	}
	token, err := r.systemClient.TenantServiceAccessToken(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get task detail service token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request task detail: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("task detail API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode task detail: %w", err)
	}
	if payloadTaskType, _ := payload["task_type"].(string); strings.TrimSpace(payloadTaskType) != taskType {
		return nil, fmt.Errorf("task detail task_type mismatch: got %q, want %q", payloadTaskType, taskType)
	}
	return payload, nil
}

func providerTaskCapability(provider *commonModels.TaskProvider, taskType string) (*taskprovider.TaskCapability, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider is nil")
	}
	if provider.Capabilities == nil || strings.TrimSpace(string(*provider.Capabilities)) == "" {
		return nil, fmt.Errorf("capabilities is required")
	}

	capabilities, err := taskprovider.ParseCapabilities(string(*provider.Capabilities))
	if err != nil {
		return nil, err
	}
	return capabilities.CapabilityFor(taskType), nil
}

func validateStepParametersByExecutionContract(step models.Step, contract *taskprovider.ExecutionContract, allowTemplateStrings bool) error {
	if contract == nil {
		return fmt.Errorf("execution_contract is required")
	}
	return taskprovider.ValidateExecutionParameters(contract.InputSchema, step.Parameters, taskprovider.ParameterValidationOptions{
		AllowTemplateStrings: allowTemplateStrings,
	})
}
