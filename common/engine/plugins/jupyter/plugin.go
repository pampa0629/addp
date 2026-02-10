package jupyter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/addp/common/engine/plugin"
)

// JupyterPlugin Jupyter 引擎插件
type JupyterPlugin struct{}

func init() {
	plugin.Register(&JupyterPlugin{})
}

func (p *JupyterPlugin) Type() string {
	return "jupyter"
}

func (p *JupyterPlugin) DisplayName() string {
	return "Jupyter Engine"
}

func (p *JupyterPlugin) EngineCategory() string {
	return "extension" // Jupyter 是扩展引擎
}

func (p *JupyterPlugin) DefaultPort() int {
	return 8097 // Jupyter API Server 默认端口
}

func (p *JupyterPlugin) RequiredFields() []string {
	return []string{"host", "port"}
}

func (p *JupyterPlugin) SensitiveFields() []string {
	return []string{} // Jupyter 引擎通常不需要敏感字段
}

func (p *JupyterPlugin) GenerateCapabilities() string {
	return `{"compute":[{"dev_modes":["notebook"],"features":["interactive","parametrized","async"],"supported_languages":["python","shell"]}]}`
}

func (p *JupyterPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *JupyterPlugin) BuildConnectionString(connInfo plugin.ConnectionInfo) (string, error) {
	// 工作流引擎返回 JSON 格式的连接信息
	bytes, err := json.Marshal(connInfo)
	if err != nil {
		return "", fmt.Errorf("failed to marshal Jupyter connection info: %w", err)
	}
	return string(bytes), nil
}

func (p *JupyterPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
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
	fmt.Printf("[JupyterPlugin] 🔗 准备连接: %s\n", healthURL)

	// 创建 HTTP 客户端，设置超时
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		fmt.Printf("[JupyterPlugin] ❌ 创建请求失败: %v\n", err)
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 发送请求
	fmt.Printf("[JupyterPlugin] 📤 发送 HTTP 请求...\n")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[JupyterPlugin] ❌ 连接失败: %v\n", err)
		return fmt.Errorf("failed to connect to Jupyter engine: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态码
	fmt.Printf("[JupyterPlugin] 📥 收到响应: status=%d\n", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status: %d", resp.StatusCode)
	}

	fmt.Printf("[JupyterPlugin] ✅ 连接测试成功\n")
	return nil
}
