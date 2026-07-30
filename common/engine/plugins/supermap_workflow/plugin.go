package supermap_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/addp/common/engine/plugin"
)

// SuperMapWorkflowPlugin exposes the SuperMap iObjects Java / SPS runtime
// through the unified ADDP workflow provider contract.
type SuperMapWorkflowPlugin struct{}

func init() {
	plugin.Register(&SuperMapWorkflowPlugin{})
}

func (p *SuperMapWorkflowPlugin) Type() string {
	return "supermap_workflow"
}

func (p *SuperMapWorkflowPlugin) DisplayName() string {
	return "SuperMap Workflow"
}

func (p *SuperMapWorkflowPlugin) EngineOrigin() string {
	return "extension"
}

func (p *SuperMapWorkflowPlugin) DefaultPort() int {
	return 8103
}

func (p *SuperMapWorkflowPlugin) RequiredFields() []string {
	return []string{"host", "port"}
}

func (p *SuperMapWorkflowPlugin) SensitiveFields() []string {
	return []string{}
}

func (p *SuperMapWorkflowPlugin) ConnectionIdentityFields() []string {
	return []string{"protocol", "host", "port"}
}

func (p *SuperMapWorkflowPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewWorkflowCapabilities(p.Type(), plugin.WorkflowRuntimeAPIAddpV1)
}

func (p *SuperMapWorkflowPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *SuperMapWorkflowPlugin) RuntimeEndpoint(ctx context.Context, connInfo plugin.ConnectionInfo) (string, error) {
	return plugin.RuntimeBaseURL(connInfo)
}

func (p *SuperMapWorkflowPlugin) ListOperators(ctx context.Context, connInfo plugin.ConnectionInfo) ([]plugin.OperatorDescriptor, error) {
	if err := p.TestConnection(ctx, connInfo); err != nil {
		return nil, err
	}
	return plugin.HTTPListOperators(ctx, connInfo)
}

func (p *SuperMapWorkflowPlugin) ExecuteWorkflow(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.WorkflowExecuteRequest) (*plugin.WorkflowExecuteResult, error) {
	return plugin.HTTPExecuteWorkflow(ctx, connInfo, req)
}

func (p *SuperMapWorkflowPlugin) InvokeOperator(ctx context.Context, connInfo plugin.ConnectionInfo, operatorName string, req plugin.OperatorInvokeRequest) (*plugin.OperatorInvokeResult, error) {
	return plugin.HTTPInvokeOperator(ctx, connInfo, operatorName, req)
}

func (p *SuperMapWorkflowPlugin) GetExecutionStatus(ctx context.Context, connInfo plugin.ConnectionInfo, executionID string) (*plugin.WorkflowExecutionStatus, error) {
	return plugin.HTTPGetExecutionStatus(ctx, connInfo, executionID)
}

func (p *SuperMapWorkflowPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	if err := p.ValidateConnectionInfo(connInfo); err != nil {
		return err
	}
	baseURL, err := plugin.RuntimeBaseURL(connInfo)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to SuperMap Workflow engine: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read health response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status %d: %s", resp.StatusCode, string(body))
	}

	var health superMapHealthResponse
	if err := json.Unmarshal(body, &health); err != nil {
		return fmt.Errorf("failed to decode SuperMap Workflow health response: %w", err)
	}
	if err := requireSuperMapDependency("objectsjava", health.Dependencies.ObjectsJava); err != nil {
		return err
	}
	if err := requireSuperMapDependency("gpa_libs", health.Dependencies.GPALibs); err != nil {
		return err
	}
	return nil
}

type superMapHealthResponse struct {
	Status       string `json:"status"`
	Ready        *bool  `json:"ready"`
	Dependencies struct {
		ObjectsJava superMapDependency `json:"objectsjava"`
		GPALibs     superMapDependency `json:"gpa_libs"`
	} `json:"dependencies"`
}

type superMapDependency struct {
	Available bool   `json:"available"`
	Path      string `json:"path"`
	Details   string `json:"details"`
}

func requireSuperMapDependency(name string, dep superMapDependency) error {
	if dep.Available {
		return nil
	}
	details := dep.Details
	if details == "" {
		details = dep.Path
	}
	switch name {
	case "objectsjava":
		return fmt.Errorf("supermap objectsjava runtime is not bound to the engine runtime: %s", details)
	case "gpa_libs":
		return fmt.Errorf("supermap GPA/SPS libs are not bound to the engine runtime: %s", details)
	default:
		return fmt.Errorf("supermap dependency %s is not available: %s", name, details)
	}
}
