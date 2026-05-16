package plugin

import "time"

func catalogNodePath(node CatalogNode) string {
	if value := catalogNodeStringAttribute(node.Attributes, "path"); value != "" {
		return value
	}
	return node.Path.StringPath()
}

func catalogNodeStringAttribute(attrs map[string]interface{}, key string) string {
	if attrs == nil {
		return ""
	}
	if storage, ok := attrs["storage"].(map[string]interface{}); ok {
		if value := catalogNodeStringAttribute(storage, key); value != "" {
			return value
		}
	}
	value, _ := attrs[key].(string)
	return value
}

func catalogNodeTimeAttribute(attrs map[string]interface{}, key string) time.Time {
	if attrs == nil {
		return time.Time{}
	}
	if storage, ok := attrs["storage"].(map[string]interface{}); ok {
		if value := catalogNodeTimeAttribute(storage, key); !value.IsZero() {
			return value
		}
	}
	switch value := attrs[key].(type) {
	case time.Time:
		return value
	case *time.Time:
		if value != nil {
			return *value
		}
	}
	return time.Time{}
}

func catalogNodeInt64Stat(stats map[string]interface{}, key string) int64 {
	if stats == nil {
		return 0
	}
	switch value := stats[key].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case uint:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}
