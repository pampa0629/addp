package models

import (
	"fmt"
	"net/url"
	"strconv"
)

// WorkflowEndpoint 工作流引擎端点配置
type WorkflowEndpoint struct {
	Method  string // HTTP 方法
	Path    string // 端点路径（支持 {id} 占位符）
	Timeout int    // 超时时间（秒）
}

// WorkflowHealthCheck 工作流引擎健康检查配置
type WorkflowHealthCheck struct {
	Endpoint string // 健康检查端点
	Timeout  int    // 超时时间（秒）
	Interval int    // 检查间隔（秒）
}

// WorkflowStandard 工作流引擎标准配置
type WorkflowStandard struct {
	Endpoints   map[string]WorkflowEndpoint
	HealthCheck WorkflowHealthCheck
}

// WorkflowStandards 各类工作流引擎的标准配置
var WorkflowStandards = map[string]WorkflowStandard{
	"python_workflow": {
		Endpoints: map[string]WorkflowEndpoint{
			"execute": {
				Method:  "POST",
				Path:    "/api/workflows/execute",
				Timeout: 300,
			},
			"status": {
				Method:  "GET",
				Path:    "/api/workflows/status/{id}",
				Timeout: 10,
			},
			"logs": {
				Method:  "GET",
				Path:    "/api/workflows/logs/{id}",
				Timeout: 10,
			},
			"cancel": {
				Method:  "POST",
				Path:    "/api/workflows/cancel/{id}",
				Timeout: 30,
			},
		},
		HealthCheck: WorkflowHealthCheck{
			Endpoint: "/health",
			Timeout:  5,
			Interval: 60,
		},
	},
	"spark_workflow": {
		Endpoints: map[string]WorkflowEndpoint{
			"execute": {
				Method:  "POST",
				Path:    "/api/workflows/execute",
				Timeout: 300,
			},
			"status": {
				Method:  "GET",
				Path:    "/api/workflows/status/{id}",
				Timeout: 10,
			},
			"logs": {
				Method:  "GET",
				Path:    "/api/workflows/logs/{id}",
				Timeout: 10,
			},
			"cancel": {
				Method:  "POST",
				Path:    "/api/workflows/cancel/{id}",
				Timeout: 30,
			},
		},
		HealthCheck: WorkflowHealthCheck{
			Endpoint: "/health",
			Timeout:  5,
			Interval: 60,
		},
	},
}

// BuildBaseURL 从 connection_info 构建 base_url
// 用于扩展引擎（engine_origin = "extension"）
func BuildBaseURL(connInfo ConnectionInfo) (string, error) {
	protocol := "http"
	if p, ok := connInfo["protocol"].(string); ok && p != "" {
		protocol = p
	}

	host, ok := connInfo["host"]
	if !ok || host == "" {
		return "", fmt.Errorf("missing required field: host")
	}

	port, ok := connInfo["port"]
	if !ok || port == nil {
		return "", fmt.Errorf("missing required field: port")
	}

	// 处理 port 的类型转换（可能是 float64 或 int 或 string）
	var portStr string
	switch v := port.(type) {
	case float64:
		portStr = strconv.Itoa(int(v))
	case int:
		portStr = strconv.Itoa(v)
	case string:
		portStr = v
	default:
		portStr = fmt.Sprintf("%v", v)
	}

	// 构建 URL
	return fmt.Sprintf("%s://%v:%s", protocol, host, portStr), nil
}

// ParseBaseURLToConnectionInfo 将 base_url 解析为 connection_info 格式
// 用于迁移和测试
func ParseBaseURLToConnectionInfo(baseURL string) (ConnectionInfo, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base_url: %w", err)
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	// 将 port 转换为整数
	portInt, err := strconv.Atoi(port)
	if err != nil {
		return nil, fmt.Errorf("invalid port: %w", err)
	}

	return ConnectionInfo{
		"protocol": u.Scheme,
		"host":     host,
		"port":     portInt,
	}, nil
}
