package service

import (
	"context"
	"errors"

	commonClient "github.com/addp/common/client"
	engineplugin "github.com/addp/common/engine/plugin"
	engineselection "github.com/addp/common/engine/selection"
	commonModels "github.com/addp/common/models"
)

// WorkflowRuntimeEngineLister projects System Runtime Descriptors into the
// existing Manager workflow selection boundary.
type WorkflowRuntimeEngineLister struct {
	system *commonClient.SystemServiceClient
}

func NewWorkflowRuntimeEngineLister(system *commonClient.SystemServiceClient) *WorkflowRuntimeEngineLister {
	return &WorkflowRuntimeEngineLister{system: system}
}

func (l *WorkflowRuntimeEngineLister) ListWorkflowEngines(tenantID uint) ([]commonModels.Engine, error) {
	if l == nil || l.system == nil {
		return nil, errors.New("System Runtime Descriptor client is required")
	}
	descriptors, err := l.system.WithTenantID(tenantID).ListEngineRuntimeDescriptors(context.Background())
	if err != nil {
		return nil, err
	}
	engines := make([]commonModels.Engine, 0, len(descriptors))
	for index := range descriptors {
		engine := descriptors[index].AsEngine()
		if !isADDPWorkflowRuntime(engine) {
			continue
		}
		engines = append(engines, *engine)
	}
	return engines, nil
}

func isADDPWorkflowRuntime(engine *commonModels.Engine) bool {
	if !engineselection.IsSelectionOptionForComputeEntrypoint(engine, "workflow") || engine.Capabilities == nil {
		return false
	}
	capabilities, err := engineplugin.ParseEngineCapabilities(string(*engine.Capabilities))
	if err != nil || capabilities.Compute == nil || capabilities.Compute.Workflow == nil {
		return false
	}
	workflow := capabilities.Compute.Workflow
	return workflow.Supported && workflow.RuntimeAPI == engineplugin.WorkflowRuntimeAPIAddpV1
}
