package spark_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/addp/common/database/plugin"
)

// SparkWorkflowPlugin Spark 工作流引擎插件
type SparkWorkflowPlugin struct{}

func init() {
	plugin.Register(&SparkWorkflowPlugin{})
}

func (p *SparkWorkflowPlugin) Type() string {
	return "spark_workflow"
}

func (p *SparkWorkflowPlugin) DisplayName() string {
	return "Spark Workflow"
}

func (p *SparkWorkflowPlugin) ConnectionCategory() string {
	return "extension"
}

func (p *SparkWorkflowPlugin) DefaultPort() int {
	return 8098
}

func (p *SparkWorkflowPlugin) RequiredFields() []string {
	return []string{"host", "port"}
}

func (p *SparkWorkflowPlugin) SensitiveFields() []string {
	return []string{} // 工作流引擎通常不需要敏感字段
}

func (p *SparkWorkflowPlugin) GenerateCapabilities() string {
	return `{"compute":[{"type":"spatial","engine":"sedona"},{"type":"general","engine":"spark"}]}`
}

func (p *SparkWorkflowPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *SparkWorkflowPlugin) BuildConnectionString(connInfo plugin.ConnectionInfo) (string, error) {
	// 工作流引擎返回 JSON 格式的连接信息
	bytes, err := json.Marshal(connInfo)
	if err != nil {
		return "", fmt.Errorf("failed to marshal Spark Workflow connection info: %w", err)
	}
	return string(bytes), nil
}

func (p *SparkWorkflowPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
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
	fmt.Printf("[SparkWorkflowPlugin] 🔗 准备连接: %s\n", healthURL)

	// 创建 HTTP 客户端，设置超时
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		fmt.Printf("[SparkWorkflowPlugin] ❌ 创建请求失败: %v\n", err)
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 发送请求
	fmt.Printf("[SparkWorkflowPlugin] 📤 发送 HTTP 请求...\n")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[SparkWorkflowPlugin] ❌ 连接失败: %v\n", err)
		return fmt.Errorf("failed to connect to Spark Workflow engine: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态码
	fmt.Printf("[SparkWorkflowPlugin] 📥 收到响应: status=%d\n", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status: %d", resp.StatusCode)
	}

	fmt.Printf("[SparkWorkflowPlugin] ✅ 连接测试成功\n")
	return nil
}
