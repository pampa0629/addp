package metacleanup

import (
	"encoding/json"
	"fmt"

	"github.com/addp/common/events"
)

func ParseCleanupRequest(values map[string]interface{}) (events.CleanupRequestEvent, error) {
	return events.ParseCleanupRequest(values)
}

func ToMap(v interface{}) map[string]interface{} {
	data, _ := json.Marshal(v)
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	return result
}

type CleanupScope struct {
	EngineID uint
}

func ScopeFromContext(values map[string]interface{}) CleanupScope {
	return CleanupScope{
		EngineID: uintFromContext(values, "engine_id"),
	}
}

func uintFromContext(values map[string]interface{}, key string) uint {
	switch value := values[key].(type) {
	case uint:
		return value
	case int:
		if value > 0 {
			return uint(value)
		}
	case int64:
		if value > 0 {
			return uint(value)
		}
	case float64:
		if value > 0 {
			return uint(value)
		}
	case string:
		var parsed uint64
		if _, err := fmt.Sscanf(value, "%d", &parsed); err == nil {
			return uint(parsed)
		}
	}
	return 0
}
