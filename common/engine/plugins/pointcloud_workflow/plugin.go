package pointcloud_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/addp/common/engine/plugin"
)

// PointCloudWorkflowPlugin 点云处理工作流引擎插件。
type PointCloudWorkflowPlugin struct{}

func init() {
	plugin.Register(&PointCloudWorkflowPlugin{})
}

func (p *PointCloudWorkflowPlugin) Type() string {
	return "pointcloud_workflow"
}

func (p *PointCloudWorkflowPlugin) DisplayName() string {
	return "PointCloud Workflow"
}

func (p *PointCloudWorkflowPlugin) EngineOrigin() string {
	return "extension"
}

func (p *PointCloudWorkflowPlugin) DefaultPort() int {
	return 8102
}

func (p *PointCloudWorkflowPlugin) RequiredFields() []string {
	return []string{"host", "port"}
}

func (p *PointCloudWorkflowPlugin) SensitiveFields() []string {
	return []string{}
}

func (p *PointCloudWorkflowPlugin) ConnectionIdentityFields() []string {
	return []string{"protocol", "host", "port"}
}

func (p *PointCloudWorkflowPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewWorkflowCapabilities(p.Type(), "addp.workflow/v1")
}

func (p *PointCloudWorkflowPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *PointCloudWorkflowPlugin) RuntimeEndpoint(ctx context.Context, connInfo plugin.ConnectionInfo) (string, error) {
	return plugin.RuntimeBaseURL(connInfo)
}

func (p *PointCloudWorkflowPlugin) ListOperators(ctx context.Context, connInfo plugin.ConnectionInfo) ([]plugin.OperatorDescriptor, error) {
	if err := p.TestConnection(ctx, connInfo); err != nil {
		return nil, err
	}
	return plugin.HTTPListOperators(ctx, connInfo)
}

func (p *PointCloudWorkflowPlugin) ExecuteWorkflow(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.WorkflowExecuteRequest) (*plugin.WorkflowExecuteResult, error) {
	return plugin.HTTPExecuteWorkflow(ctx, connInfo, req)
}

func (p *PointCloudWorkflowPlugin) InvokeOperator(ctx context.Context, connInfo plugin.ConnectionInfo, operatorName string, req plugin.OperatorInvokeRequest) (*plugin.OperatorInvokeResult, error) {
	return plugin.HTTPInvokeOperator(ctx, connInfo, operatorName, req)
}

func (p *PointCloudWorkflowPlugin) GetExecutionStatus(ctx context.Context, connInfo plugin.ConnectionInfo, executionID string) (*plugin.WorkflowExecutionStatus, error) {
	return plugin.HTTPGetExecutionStatus(ctx, connInfo, executionID)
}

func (p *PointCloudWorkflowPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to PointCloud Workflow engine: %w", err)
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
			PDAL struct {
				Available bool   `json:"available"`
				Path      string `json:"path"`
				Details   string `json:"details"`
			} `json:"pdal"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(body, &health); err != nil {
		return fmt.Errorf("failed to decode PointCloud Workflow health response: %w", err)
	}
	if health.ConversionReady != nil && !*health.ConversionReady {
		details := health.Dependencies.PDAL.Details
		if details == "" {
			details = health.Dependencies.PDAL.Path
		}
		return fmt.Errorf("pointcloud PDAL runtime is not bound to the engine runtime: %s", details)
	}
	return nil
}
