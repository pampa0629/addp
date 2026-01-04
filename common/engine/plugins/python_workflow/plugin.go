package python_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/addp/common/engine/plugin"
)

// PythonWorkflowPlugin Python 工作流引擎插件
type PythonWorkflowPlugin struct{}

func init() {
	plugin.Register(&PythonWorkflowPlugin{})
}

func (p *PythonWorkflowPlugin) Type() string {
	return "python_workflow"
}

func (p *PythonWorkflowPlugin) DisplayName() string {
	return "Python Workflow"
}

func (p *PythonWorkflowPlugin) EngineCategory() string {
	return "extension"
}

func (p *PythonWorkflowPlugin) DefaultPort() int {
	return 8099
}

func (p *PythonWorkflowPlugin) RequiredFields() []string {
	return []string{"host", "port"}
}

func (p *PythonWorkflowPlugin) SensitiveFields() []string {
	return []string{} // 工作流引擎通常不需要敏感字段
}

func (p *PythonWorkflowPlugin) GenerateCapabilities() string {
	return `{"compute":[{"type":"spatial","engine":"geopandas"},{"type":"general","engine":"pandas"}]}`
}

func (p *PythonWorkflowPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *PythonWorkflowPlugin) BuildConnectionString(connInfo plugin.ConnectionInfo) (string, error) {
	// 工作流引擎返回 JSON 格式的连接信息
	bytes, err := json.Marshal(connInfo)
	if err != nil {
		return "", fmt.Errorf("failed to marshal Python Workflow connection info: %w", err)
	}
	return string(bytes), nil
}

func (p *PythonWorkflowPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
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
	fmt.Printf("[PythonWorkflowPlugin] 🔗 准备连接: %s\n", healthURL)

	// 创建 HTTP 客户端，设置超时
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		fmt.Printf("[PythonWorkflowPlugin] ❌ 创建请求失败: %v\n", err)
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 发送请求
	fmt.Printf("[PythonWorkflowPlugin] 📤 发送 HTTP 请求...\n")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[PythonWorkflowPlugin] ❌ 连接失败: %v\n", err)
		return fmt.Errorf("failed to connect to Python Workflow engine: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态码
	fmt.Printf("[PythonWorkflowPlugin] 📥 收到响应: status=%d\n", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status: %d", resp.StatusCode)
	}

	fmt.Printf("[PythonWorkflowPlugin] ✅ 连接测试成功\n")
	return nil
}

// === ComputePlugin 接口实现 ===

// GetSupportedOperators 获取支持的算子列表
func (p *PythonWorkflowPlugin) GetSupportedOperators() []string {
	return []string{
		// 空间算子
		"buffer", "intersection", "union", "difference", "dissolve",
		"clip", "overlay", "spatial_join", "centroid", "convex_hull",
		// 数据处理算子
		"filter", "select", "groupby", "join", "aggregate",
		"sort", "rename", "drop", "fillna", "dropna",
	}
}

// HealthCheckEndpoint 健康检查端点
func (p *PythonWorkflowPlugin) HealthCheckEndpoint() string {
	return "/health"
}
