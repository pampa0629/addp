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

// NewOperatorDiscoveryService 创建算子发现服务
func NewOperatorDiscoveryService(systemClient *commonClient.SystemClient) *OperatorDiscoveryService {
	return &OperatorDiscoveryService{
		getEngineByID:         systemClient.GetEngineByID,
		listWorkflowOperators: dbbridge.ListWorkflowOperators,
	}
}

// GetOperatorsByWorkflowEngineID 根据具体工作流运行时引擎实例获取算子。
func (s *OperatorDiscoveryService) GetOperatorsByWorkflowEngineID(ctx context.Context, workflowEngineID uint) ([]commonModels.OperatorDescriptor, error) {
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

	return workflowCapableOperators(operators), nil
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
