package plugin

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ConnectionIdentityDefinition 返回插件声明的永久实例身份字段。
// 未编译进当前进程的扩展运行时统一使用 addp.workflow/v1 HTTP 端点身份。
func ConnectionIdentityDefinition(engineType string) ([]string, EnginePlugin, error) {
	enginePlugin, err := Get(strings.TrimSpace(engineType))
	if err == nil {
		provider, ok := enginePlugin.(ConnectionIdentityProvider)
		if !ok {
			return nil, nil, fmt.Errorf("engine plugin %s did not implement ConnectionIdentityProvider", engineType)
		}
		fields := provider.ConnectionIdentityFields()
		if len(fields) == 0 {
			return nil, nil, fmt.Errorf("engine plugin %s did not declare connection identity fields", engineType)
		}
		return fields, enginePlugin, nil
	}
	return []string{"protocol", "host", "port"}, nil, nil
}

// BuildConnectionIdentityKey 生成与运行环境无关、可持久化为 JSONB 的身份键。
func BuildConnectionIdentityKey(engineType string, connInfo ConnectionInfo) (string, error) {
	fields, enginePlugin, err := ConnectionIdentityDefinition(engineType)
	if err != nil {
		return "", err
	}
	identity := make(map[string]string, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			return "", fmt.Errorf("engine plugin %s declared an empty connection identity field", engineType)
		}
		identity[field] = NormalizeConnectionIdentityValue(field, connInfo, enginePlugin)
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("marshal engine connection identity: %w", err)
	}
	return string(encoded), nil
}

// NormalizeConnectionIdentityValue 只处理持久身份语义，不能读取部署环境变量。
func NormalizeConnectionIdentityValue(field string, connInfo ConnectionInfo, enginePlugin EnginePlugin) string {
	field = strings.TrimSpace(field)
	switch field {
	case "port":
		port := GetInt(connInfo, field)
		if port == 0 && enginePlugin != nil {
			port = enginePlugin.DefaultPort()
		}
		return fmt.Sprintf("%d", port)
	case "protocol":
		value := strings.ToLower(strings.TrimSpace(GetString(connInfo, field)))
		if value == "" {
			value = "http"
		}
		return value
	case "host", "server":
		value := strings.ToLower(strings.TrimSpace(GetString(connInfo, field)))
		switch value {
		case "localhost", "127.0.0.1", "host.docker.internal":
			return "127.0.0.1"
		default:
			return value
		}
	case "endpoint":
		return strings.ToLower(strings.TrimRight(strings.TrimSpace(GetString(connInfo, field)), "/"))
	case "export_path":
		value := strings.TrimSpace(GetString(connInfo, field))
		if value == "/" {
			return value
		}
		return strings.TrimRight(value, "/")
	case "auth_source":
		value := strings.TrimSpace(GetString(connInfo, field))
		if value == "" && enginePlugin != nil && enginePlugin.Type() == "mongodb" {
			value = "admin"
		}
		return value
	default:
		return strings.TrimSpace(GetString(connInfo, field))
	}
}
