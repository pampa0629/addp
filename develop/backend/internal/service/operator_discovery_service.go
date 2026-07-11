package service

import (
	"context"
	"fmt"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/utils"
)

// OperatorDiscoveryService 工作流算子发现服务
// 负责从所有注册的工作流运行时引擎获取算子列表并合并缓存。
type OperatorDiscoveryService struct {
	getEngineByID         func(uint) (*commonModels.Engine, error)
	listWorkflowOperators func(context.Context, *commonModels.Engine) ([]commonModels.OperatorDescriptor, error)
}

type PublicOperatorDescriptor struct {
	commonModels.OperatorDescriptor
	PublicParameters []commonModels.ParameterDescriptor `json:"public_parameters"`
}

// NewOperatorDiscoveryService 创建算子发现服务
func NewOperatorDiscoveryService(systemClient *commonClient.SystemClient) *OperatorDiscoveryService {
	return &OperatorDiscoveryService{
		getEngineByID:         systemClient.GetEngineByID,
		listWorkflowOperators: dbbridge.ListWorkflowOperators,
	}
}

// GetOperatorsByWorkflowEngineID 根据具体工作流运行时引擎实例获取算子。
func (s *OperatorDiscoveryService) GetOperatorsByWorkflowEngineID(ctx context.Context, workflowEngineID uint) ([]PublicOperatorDescriptor, error) {
	if workflowEngineID == 0 {
		return nil, fmt.Errorf("workflow_engine_id 必须大于 0")
	}

	engine, err := s.getEngineByID(workflowEngineID)
	if err != nil {
		return nil, fmt.Errorf("查询工作流引擎失败: %w", err)
	}
	if engine == nil {
		return nil, fmt.Errorf("工作流引擎不存在: %d", workflowEngineID)
	}
	if !engine.IsActive {
		return nil, fmt.Errorf("工作流引擎未启用: %d", workflowEngineID)
	}
	if !utils.SupportsComputeEntrypoint(engine, "workflow") {
		return nil, fmt.Errorf("引擎 %d 不具备 compute.workflow 能力", workflowEngineID)
	}

	operators, err := s.listWorkflowOperators(ctx, engine)
	if err != nil {
		return nil, fmt.Errorf("引擎 %s 获取算子失败: %w", engine.Name, err)
	}

	workflowOperators := workflowCapableOperators(operators)
	if err := validateWorkflowOperatorContracts(engine.EngineType, workflowOperators); err != nil {
		return nil, err
	}
	return publicWorkflowOperators(engine.EngineType, workflowOperators), nil
}

func validateWorkflowOperatorContracts(engineType string, operators []commonModels.OperatorDescriptor) error {
	for _, operator := range operators {
		parameterNames := make(map[string]struct{}, len(operator.Parameters))
		for _, parameter := range operator.Parameters {
			if _, duplicated := parameterNames[parameter.Name]; duplicated {
				return fmt.Errorf("workflow engine %s operator %s 的 Runtime Operator Spec 重复声明参数 %s", engineType, operator.ID, parameter.Name)
			}
			if parameter.UIType == "resource_tree_picker" || parameter.ParamType == "ui" {
				return fmt.Errorf("workflow engine %s operator %s 的 Runtime Operator Spec 不允许包含 UI 参数 %s", engineType, operator.ID, parameter.Name)
			}
			switch parameter.Name {
			case "locator", "target_parent_locator", "target_name":
				return fmt.Errorf("workflow engine %s operator %s 的 Runtime Operator Spec 不允许包含公开资源参数 %s", engineType, operator.ID, parameter.Name)
			}
			parameterNames[parameter.Name] = struct{}{}
		}

		adapterSpec, hasAdapterSpec := workflowOperatorAdapterSpecFor(engineType, operator.ID)
		if !hasAdapterSpec {
			continue
		}
		for runtimeParam := range workflowAdapterRuntimeParams(adapterSpec) {
			if runtimeParam == "engine_id" {
				continue
			}
			if _, ok := parameterNames[runtimeParam]; !ok {
				return fmt.Errorf("workflow engine %s operator %s 缺少 adapter spec 声明的运行时参数 %s", engineType, operator.ID, runtimeParam)
			}
		}
	}
	return nil
}

func publicWorkflowOperators(engineType string, operators []commonModels.OperatorDescriptor) []PublicOperatorDescriptor {
	result := make([]PublicOperatorDescriptor, 0, len(operators))
	for _, operator := range operators {
		result = append(result, PublicOperatorDescriptor{
			OperatorDescriptor: operator,
			PublicParameters:   publicOperatorParameters(engineType, operator),
		})
	}
	return result
}

func publicOperatorParameters(engineType string, operator commonModels.OperatorDescriptor) []commonModels.ParameterDescriptor {
	adapterSpec, _ := workflowOperatorAdapterSpecFor(engineType, operator.ID)
	runtimeParams := workflowAdapterRuntimeParams(adapterSpec)
	result := make([]commonModels.ParameterDescriptor, 0, len(operator.Parameters)+len(adapterSpec.PublicParameters))
	for _, parameter := range operator.Parameters {
		if _, internal := runtimeParams[parameter.Name]; internal {
			continue
		}
		result = append(result, parameter)
	}
	result = append(result, adapterSpec.PublicParameters...)
	return result
}

func workflowCapableOperators(operators []commonModels.OperatorDescriptor) []commonModels.OperatorDescriptor {
	result := make([]commonModels.OperatorDescriptor, 0, len(operators))
	for _, operator := range operators {
		if operatorSupportsExecutionMode(operator, "workflow") {
			result = append(result, operator)
		}
	}
	return result
}

func operatorSupportsExecutionMode(operator commonModels.OperatorDescriptor, mode string) bool {
	for _, item := range operator.ExecutionModes {
		if item == mode {
			return true
		}
	}
	return false
}
