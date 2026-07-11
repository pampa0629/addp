package geopython_workflow

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/addp/common/engine/plugin"
)

// GeoPythonWorkflowPlugin GeoPython 工作流引擎插件
type GeoPythonWorkflowPlugin struct{}

func init() {
	plugin.Register(&GeoPythonWorkflowPlugin{})
}

func (p *GeoPythonWorkflowPlugin) Type() string {
	return "geopython_workflow"
}

func (p *GeoPythonWorkflowPlugin) DisplayName() string {
	return "GeoPython 工作流引擎"
}

func (p *GeoPythonWorkflowPlugin) EngineOrigin() string {
	return "extension"
}

func (p *GeoPythonWorkflowPlugin) DefaultPort() int {
	return 8099
}

func (p *GeoPythonWorkflowPlugin) RequiredFields() []string {
	return []string{"host", "port"}
}

func (p *GeoPythonWorkflowPlugin) SensitiveFields() []string {
	return []string{} // 工作流引擎通常不需要敏感字段
}

func (p *GeoPythonWorkflowPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewWorkflowCapabilities(p.Type(), "addp.workflow/v1")
}

func (p *GeoPythonWorkflowPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *GeoPythonWorkflowPlugin) RuntimeEndpoint(ctx context.Context, connInfo plugin.ConnectionInfo) (string, error) {
	return plugin.RuntimeBaseURL(connInfo)
}

func (p *GeoPythonWorkflowPlugin) ListOperators(ctx context.Context, connInfo plugin.ConnectionInfo) ([]plugin.OperatorDescriptor, error) {
	return plugin.HTTPListOperators(ctx, connInfo)
}

func (p *GeoPythonWorkflowPlugin) ExecuteWorkflow(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.WorkflowExecuteRequest) (*plugin.WorkflowExecuteResult, error) {
	return plugin.HTTPExecuteWorkflow(ctx, connInfo, req)
}

func (p *GeoPythonWorkflowPlugin) InvokeOperator(ctx context.Context, connInfo plugin.ConnectionInfo, operatorName string, req plugin.OperatorInvokeRequest) (*plugin.OperatorInvokeResult, error) {
	return plugin.HTTPInvokeOperator(ctx, connInfo, operatorName, req)
}

func (p *GeoPythonWorkflowPlugin) GetExecutionStatus(ctx context.Context, connInfo plugin.ConnectionInfo, executionID string) (*plugin.WorkflowExecutionStatus, error) {
	return plugin.HTTPGetExecutionStatus(ctx, connInfo, executionID)
}

func (p *GeoPythonWorkflowPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	host := plugin.NormalizeHost(plugin.GetString(connInfo, "host"))
	port := plugin.GetInt(connInfo, "port")
	protocol := plugin.GetString(connInfo, "protocol")

	if protocol == "" {
		protocol = "http"
	}

	if host == "" || port == 0 {
		return fmt.Errorf("missing required fields: host, port")
	}

	// 构建健康检查 URL
	healthURL := fmt.Sprintf("%s://%s:%d/health", protocol, host, port)
	fmt.Printf("[GeoPythonWorkflowPlugin] 🔗 准备连接: %s\n", healthURL)

	// 创建 HTTP 客户端，设置超时
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		fmt.Printf("[GeoPythonWorkflowPlugin] ❌ 创建请求失败: %v\n", err)
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 发送请求
	fmt.Printf("[GeoPythonWorkflowPlugin] 📤 发送 HTTP 请求...\n")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[GeoPythonWorkflowPlugin] ❌ 连接失败: %v\n", err)
		return fmt.Errorf("failed to connect to GeoPython workflow engine: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态码
	fmt.Printf("[GeoPythonWorkflowPlugin] 📥 收到响应: status=%d\n", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status: %d", resp.StatusCode)
	}

	fmt.Printf("[GeoPythonWorkflowPlugin] ✅ 连接测试成功\n")
	return nil
}
