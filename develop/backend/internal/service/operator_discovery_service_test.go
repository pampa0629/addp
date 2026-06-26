package service

import (
	"context"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
)

func TestOperatorDiscoveryReturnsWorkflowCapableOperatorsOnly(t *testing.T) {
	capabilities, err := plugin.MarshalEngineCapabilities(plugin.NewWorkflowCapabilities("acme_geo_workflow", "addp.workflow/v1"))
	if err != nil {
		t.Fatalf("MarshalEngineCapabilities() error = %v", err)
	}
	capabilitiesJSON := commonModels.JSONString(capabilities)
	engine := &commonModels.Engine{
		ID:           12,
		Name:         "acme geo workflow",
		EngineType:   "acme_geo_workflow",
		IsActive:     true,
		Capabilities: &capabilitiesJSON,
	}

	service := &OperatorDiscoveryService{
		getEngineByID: func(id uint) (*commonModels.Engine, error) {
			if id != engine.ID {
				t.Fatalf("engine id = %d, want %d", id, engine.ID)
			}
			return engine, nil
		},
		listWorkflowOperators: func(ctx context.Context, engine *commonModels.Engine) ([]commonModels.OperatorDescriptor, error) {
			return []commonModels.OperatorDescriptor{
				{
					ID:             "load",
					Name:           "load",
					ExecutionModes: []string{"workflow"},
				},
				{
					ID:             "tiff_to_cog",
					Name:           "tiff_to_cog",
					ExecutionModes: []string{"workflow", "direct"},
				},
				{
					ID:             "direct_only",
					Name:           "direct_only",
					ExecutionModes: []string{"direct"},
				},
			}, nil
		},
	}

	operators, err := service.GetOperatorsByWorkflowEngineID(context.Background(), engine.ID)
	if err != nil {
		t.Fatalf("GetOperatorsByWorkflowEngineID() error = %v", err)
	}
	if len(operators) != 2 {
		t.Fatalf("operators len = %d, want 2: %+v", len(operators), operators)
	}
	if operators[0].Name != "load" || operators[1].Name != "tiff_to_cog" {
		t.Fatalf("unexpected operators: %+v", operators)
	}
}
