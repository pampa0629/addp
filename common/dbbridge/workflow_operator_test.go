package dbbridge

import (
	"context"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

type workflowOperatorProvider struct{}

func (p *workflowOperatorProvider) Type() string { return "test_workflow_bridge" }

func (p *workflowOperatorProvider) DisplayName() string { return "Test Workflow Bridge" }

func (p *workflowOperatorProvider) EngineOrigin() string { return "extension" }

func (p *workflowOperatorProvider) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	return nil
}

func (p *workflowOperatorProvider) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return nil
}

func (p *workflowOperatorProvider) DefaultPort() int { return 0 }

func (p *workflowOperatorProvider) RequiredFields() []string { return nil }

func (p *workflowOperatorProvider) SensitiveFields() []string { return nil }

func (p *workflowOperatorProvider) Capabilities() plugin.EngineCapabilities {
	return plugin.NewWorkflowCapabilities(p.Type(), "addp.workflow/v1")
}

func (p *workflowOperatorProvider) RuntimeEndpoint(ctx context.Context, connInfo plugin.ConnectionInfo) (string, error) {
	return "http://localhost:1", nil
}

func (p *workflowOperatorProvider) ListOperators(ctx context.Context, connInfo plugin.ConnectionInfo) ([]plugin.OperatorDescriptor, error) {
	min := 1.0
	return []plugin.OperatorDescriptor{
		{
			ID:               "buffer",
			Name:             "buffer",
			DisplayName:      "缓冲区",
			Type:             "spatial",
			Category:         "空间分析",
			Description:      "生成缓冲区",
			BriefDescription: "围绕输入几何生成指定距离的缓冲区",
			DetailedDescription: map[string]interface{}{
				"overview": "缓冲区分析",
			},
			Parameters: []plugin.ParameterDescriptor{
				{
					Name:        "distance",
					Type:        "float",
					Required:    true,
					Description: "缓冲距离",
					Min:         &min,
					Properties: map[string]plugin.ParameterDescriptor{
						"unit": {
							Name:        "unit",
							Type:        "string",
							Description: "距离单位",
						},
					},
				},
			},
			Inputs: []interface{}{
				"geodataframe",
				map[string]interface{}{"type": "dataframe"},
			},
			OutputPorts: []plugin.OutputPortDescriptor{
				{
					Name:        "default",
					Type:        "geodataframe",
					Description: "缓冲区结果",
					IsDefault:   true,
				},
			},
		},
	}, nil
}

func (p *workflowOperatorProvider) ExecuteWorkflow(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.WorkflowExecuteRequest) (*plugin.WorkflowExecuteResult, error) {
	return &plugin.WorkflowExecuteResult{Status: "success"}, nil
}

func TestListWorkflowOperatorsPreservesOperatorDescriptor(t *testing.T) {
	provider := &workflowOperatorProvider{}
	plugin.Register(provider)
	defer plugin.Unregister(provider.Type())

	engine := &models.Engine{
		EngineType: provider.Type(),
		ConnectionInfo: models.ConnectionInfo{
			"host": "localhost",
			"port": 1,
		},
	}

	operators, err := ListWorkflowOperators(context.Background(), engine)
	if err != nil {
		t.Fatalf("ListWorkflowOperators returned error: %v", err)
	}
	if len(operators) != 1 {
		t.Fatalf("expected 1 operator, got %d", len(operators))
	}

	op := operators[0]
	if op.ID != "buffer" || op.Name != "buffer" || op.Module != provider.Type() {
		t.Fatalf("unexpected operator identity: %+v", op)
	}
	if op.BriefDescription == "" || op.DetailedDescription["overview"] != "缓冲区分析" {
		t.Fatalf("operator descriptions were not preserved: %+v", op)
	}
	if len(op.Parameters) != 1 || op.Parameters[0].Min == nil || op.Parameters[0].Properties["unit"].Type != "string" {
		t.Fatalf("operator parameters were not preserved: %+v", op.Parameters)
	}
	if len(op.Inputs) != 2 || op.Inputs[0] != "geodataframe" || op.Inputs[1] != "dataframe" {
		t.Fatalf("operator inputs were not converted as expected: %+v", op.Inputs)
	}
	if len(op.OutputPorts) != 1 || !op.OutputPorts[0].IsDefault {
		t.Fatalf("operator output ports were not preserved: %+v", op.OutputPorts)
	}
}
