package math_workflow

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/addp/common/engine/plugin"
)

// MathWorkflowPlugin Math 工作流引擎插件
type MathWorkflowPlugin struct{}

func init() {
	plugin.Register(&MathWorkflowPlugin{})
}

func (p *MathWorkflowPlugin) Type() string {
	return "math_workflow"
}

func (p *MathWorkflowPlugin) DisplayName() string {
	return "Math Workflow"
}

func (p *MathWorkflowPlugin) EngineOrigin() string {
	return "extension" // Math Workflow 是扩展引擎
}

func (p *MathWorkflowPlugin) DefaultPort() int {
	return 8089 // Math Workflow Engine 默认端口
}

func (p *MathWorkflowPlugin) RequiredFields() []string {
	return []string{"host", "port"}
}

func (p *MathWorkflowPlugin) SensitiveFields() []string {
	return []string{} // 工作流引擎通常不需要敏感字段
}

func (p *MathWorkflowPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewWorkflowCapabilities(p.Type(), "addp.workflow/v1")
}

func (p *MathWorkflowPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *MathWorkflowPlugin) RuntimeEndpoint(ctx context.Context, connInfo plugin.ConnectionInfo) (string, error) {
	return plugin.RuntimeBaseURL(connInfo)
}

func (p *MathWorkflowPlugin) ListOperators(ctx context.Context, connInfo plugin.ConnectionInfo) ([]plugin.OperatorDescriptor, error) {
	return plugin.HTTPListOperators(ctx, connInfo)
}

func (p *MathWorkflowPlugin) ExecuteWorkflow(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.WorkflowExecuteRequest) (*plugin.WorkflowExecuteResult, error) {
	return plugin.HTTPExecuteWorkflow(ctx, connInfo, req)
}

func (p *MathWorkflowPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
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
	fmt.Printf("[MathWorkflowPlugin] 🔗 准备连接: %s\n", healthURL)

	// 创建 HTTP 客户端，设置超时
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		fmt.Printf("[MathWorkflowPlugin] ❌ 创建请求失败: %v\n", err)
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 发送请求
	fmt.Printf("[MathWorkflowPlugin] 📤 发送 HTTP 请求...\n")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[MathWorkflowPlugin] ❌ 连接失败: %v\n", err)
		return fmt.Errorf("failed to connect to Math Workflow engine: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态码
	fmt.Printf("[MathWorkflowPlugin] 📥 收到响应: status=%d\n", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status: %d", resp.StatusCode)
	}

	fmt.Printf("[MathWorkflowPlugin] ✅ 连接测试成功\n")
	return nil
}
