package service

import (
	"strings"

	commonModels "github.com/addp/common/models"
)

func sanitizeConnectionInfo(info commonModels.ConnectionInfo) map[string]any {
	if info == nil || len(info) == 0 {
		return nil
	}
	result := make(map[string]any, len(info))
	for key, value := range info {
		lowerKey := strings.ToLower(key)
		if isSensitiveKey(lowerKey) {
			result[key] = "******"
			continue
		}

		switch v := value.(type) {
		case map[string]any:
			result[key] = sanitizeConnectionInfo(commonModels.ConnectionInfo(v))
		case []interface{}:
			result[key] = sanitizeSlice(v)
		default:
			result[key] = v
		}
	}
	return result
}

func sanitizeSlice(values []interface{}) []interface{} {
	if len(values) == 0 {
		return nil
	}
	result := make([]interface{}, len(values))
	for i, item := range values {
		switch v := item.(type) {
		case map[string]any:
			result[i] = sanitizeConnectionInfo(commonModels.ConnectionInfo(v))
		case []interface{}:
			result[i] = sanitizeSlice(v)
		default:
			result[i] = v
		}
	}
	return result
}

func isSensitiveKey(key string) bool {
	return strings.Contains(key, "password") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "token") ||
		strings.Contains(key, "key")
}

func connectionLogFields(resource *commonModels.Engine) []any {
	fields := []any{
		"engine_id", resource.ID,
		"tenant_id", resource.TenantID,
		"resource_name", resource.Name,
		"resource_type", strings.ToLower(resource.EngineType),
	}

	if sanitized := sanitizeConnectionInfo(resource.ConnectionInfo); sanitized != nil {
		fields = append(fields, "connection_info", sanitized)
	}

	return fields
}

func cloneLogFields(base []any, extras ...any) []any {
	out := make([]any, len(base), len(base)+len(extras))
	copy(out, base)
	return append(out, extras...)
}
