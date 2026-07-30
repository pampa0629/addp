package model3d_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/addp/common/engine/plugin"
)

// Model3DWorkflowPlugin 三维模型工作流引擎插件。
type Model3DWorkflowPlugin struct{}

func init() {
	plugin.Register(&Model3DWorkflowPlugin{})
}

func (p *Model3DWorkflowPlugin) Type() string {
	return "model3d_workflow"
}

func (p *Model3DWorkflowPlugin) DisplayName() string {
	return "Model3D Workflow"
}

func (p *Model3DWorkflowPlugin) EngineOrigin() string {
	return "extension"
}

func (p *Model3DWorkflowPlugin) DefaultPort() int {
	return 8101
}

func (p *Model3DWorkflowPlugin) RequiredFields() []string {
	return []string{"host", "port"}
}

func (p *Model3DWorkflowPlugin) SensitiveFields() []string {
	return []string{}
}

func (p *Model3DWorkflowPlugin) ConnectionIdentityFields() []string {
	return []string{"protocol", "host", "port"}
}

func (p *Model3DWorkflowPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewWorkflowCapabilities(p.Type(), "addp.workflow/v1")
}

func (p *Model3DWorkflowPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *Model3DWorkflowPlugin) RuntimeEndpoint(ctx context.Context, connInfo plugin.ConnectionInfo) (string, error) {
	return plugin.RuntimeBaseURL(connInfo)
}

func (p *Model3DWorkflowPlugin) ListOperators(ctx context.Context, connInfo plugin.ConnectionInfo) ([]plugin.OperatorDescriptor, error) {
	if err := p.TestConnection(ctx, connInfo); err != nil {
		return nil, err
	}
	return plugin.HTTPListOperators(ctx, connInfo)
}

func (p *Model3DWorkflowPlugin) ExecuteWorkflow(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.WorkflowExecuteRequest) (*plugin.WorkflowExecuteResult, error) {
	return plugin.HTTPExecuteWorkflow(ctx, connInfo, req)
}

func (p *Model3DWorkflowPlugin) InvokeOperator(ctx context.Context, connInfo plugin.ConnectionInfo, operatorName string, req plugin.OperatorInvokeRequest) (*plugin.OperatorInvokeResult, error) {
	return plugin.HTTPInvokeOperator(ctx, connInfo, operatorName, req)
}

func (p *Model3DWorkflowPlugin) GetExecutionStatus(ctx context.Context, connInfo plugin.ConnectionInfo, executionID string) (*plugin.WorkflowExecutionStatus, error) {
	return plugin.HTTPGetExecutionStatus(ctx, connInfo, executionID)
}

func (p *Model3DWorkflowPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	host := plugin.NormalizeHost(plugin.GetString(connInfo, "host"))
	port := plugin.GetInt(connInfo, "port")
	protocol := plugin.GetString(connInfo, "protocol")

	if protocol == "" {
		protocol = "http"
	}

	if host == "" || port == 0 {
		return fmt.Errorf("missing required fields: host, port")
	}

	healthURL := fmt.Sprintf("%s://%s:%d/health", protocol, host, port)
	client := &http.Client{Timeout: 5 * time.Second}

	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Model3D Workflow engine: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read health response: %w", err)
	}
	var health struct {
		ConversionReady *bool `json:"conversion_ready"`
		Dependencies    struct {
			Converter struct {
				Available bool   `json:"available"`
				Path      string `json:"path"`
				Details   string `json:"details"`
			} `json:"converter"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(body, &health); err != nil {
		return fmt.Errorf("failed to decode Model3D Workflow health response: %w", err)
	}
	if health.ConversionReady != nil && !*health.ConversionReady {
		details := health.Dependencies.Converter.Details
		if details == "" {
			details = health.Dependencies.Converter.Path
		}
		return fmt.Errorf("model3d converter is not bound to the engine runtime: %s", details)
	}

	return nil
}
