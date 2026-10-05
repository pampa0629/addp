package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"strings"

	"github.com/google/uuid"
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

// RuntimeNodeIdentity separates deployment-provided host nodes from OS hostnames.
// An unavailable hostname remains unknown and does not affect readiness.
func RuntimeNodeIdentity() (hostNodeName, runtimeHostname string) {
	runtimeHostname, _ = os.Hostname()
	return strings.TrimSpace(os.Getenv("ADDP_HOST_NODE_NAME")), strings.TrimSpace(runtimeHostname)
}

// RuntimeHostNodeIPs reads deployment-provided addresses without selecting interfaces.
// Invalid values remain in the request so System rejects the registration.
func RuntimeHostNodeIPs() []string {
	value := strings.TrimSpace(os.Getenv("ADDP_HOST_NODE_IPS"))
	if value == "" {
		return []string{}
	}
	return strings.Split(value, ",")
}

func NormalizeHostNodeIPs(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		address, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil || address.Zone() != "" {
			return nil, fmt.Errorf("invalid host node IP")
		}
		canonical := address.Unmap().String()
		if !seen[canonical] {
			seen[canonical] = true
			result = append(result, canonical)
		}
	}
	return result, nil
}

// RuntimeHostNodeID accepts only an explicit UUID; an invalid optional declaration never blocks registration.
func RuntimeHostNodeID() string {
	raw := strings.TrimSpace(os.Getenv("ADDP_HOST_NODE_ID"))
	if raw == "" {
		return ""
	}
	id, err := uuid.Parse(raw)
	if err != nil || len(raw) != 36 || id == uuid.Nil {
		slog.Warn("optional node binding omitted", "error_code", "host_node_id_invalid")
		return ""
	}
	return id.String()
}
