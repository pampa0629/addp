package utils

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// CheckPortAvailable 检查端口是否可用
// 参数 port 可以是纯数字（如 "8180"）或带冒号（如 ":8180"）
func CheckPortAvailable(port string) error {
	// 标准化端口格式
	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}

	// 尝试监听端口
	ln, err := net.Listen("tcp", port)
	if err != nil {
		return fmt.Errorf("port %s is already in use or cannot be bound: %w", port, err)
	}
	defer ln.Close()

	return nil
}

// GetModulePort 获取模块端口（统一逻辑）
// moduleName: system, manager, meta, transfer, orchestrator, develop, service, gateway
func GetModulePort(moduleName string) string {
	// 标准化模块名（转大写）
	moduleNameUpper := strings.ToUpper(moduleName)

	// Gateway 特殊处理（命名为 GATEWAY_PORT 而非 GATEWAY_BACKEND_PORT）
	var envVar string
	if moduleName == "gateway" {
		envVar = "GATEWAY_PORT"
	} else {
		envVar = moduleNameUpper + "_BACKEND_PORT"
	}

	// 默认端口映射
	defaultPorts := map[string]string{
		"system":       "8180",
		"manager":      "8081",
		"meta":         "8082",
		"transfer":     "8083",
		"orchestrator": "8084",
		"develop":      "8185",
		"service":      "8086",
		"copilot":      "8087",
		"monitor":      "8100",
		"standard":     "8110",
		"model":        "8181",
		"agent":        "8190",
		"graph":        "8186",
		"gateway":      "8000",
	}

	// 从环境变量读取，如果不存在则使用默认值
	port := os.Getenv(envVar)
	if port == "" {
		port = defaultPorts[moduleName]
	}

	// 去除可能的冒号前缀
	port = strings.TrimPrefix(port, ":")

	return port
}

// BuildServiceURL 构建服务URL
// host: localhost、host.docker.internal 或服务名
// port: 纯数字端口（如 "8180"）
func BuildServiceURL(host, port string) string {
	// 去除可能的冒号前缀
	port = strings.TrimPrefix(port, ":")

	// 如果 host 已经包含端口，直接返回
	if strings.Contains(host, ":") {
		return "http://" + host
	}

	return fmt.Sprintf("http://%s:%s", host, port)
}

// GetServiceHost 获取服务统一 host
// 开发环境: localhost
// Docker环境: host.docker.internal 或容器服务名
func GetServiceHost() string {
	host := os.Getenv("SERVICE_HOST")
	if host == "" {
		host = "localhost"
	}
	return host
}
