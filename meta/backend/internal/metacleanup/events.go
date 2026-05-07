package metacleanup

import (
	"encoding/json"
	"fmt"

	"github.com/addp/common/events"
)

func ParseCleanupRequest(values map[string]interface{}) (events.CleanupRequestEvent, error) {
	normalized := copyMap(values)

	if modulesStr, ok := normalized["expected_modules"].(string); ok && modulesStr != "" {
		var modules []string
		if err := json.Unmarshal([]byte(modulesStr), &modules); err == nil {
			normalized["expected_modules"] = modules
		}
	}

	normalizeUintField(normalized, "tenant_id")
	normalizeUintField(normalized, "requested_by")

	var event events.CleanupRequestEvent
	eventJSON, _ := json.Marshal(normalized)
	if err := json.Unmarshal(eventJSON, &event); err != nil {
		return events.CleanupRequestEvent{}, err
	}
	return event, nil
}

func ParseEngineDeleted(values map[string]interface{}) (events.EngineDeletedEvent, error) {
	var event events.EngineDeletedEvent
	eventJSON, _ := json.Marshal(values)
	if err := json.Unmarshal(eventJSON, &event); err != nil {
		return events.EngineDeletedEvent{}, err
	}
	return event, nil
}

func ToMap(v interface{}) map[string]interface{} {
	data, _ := json.Marshal(v)
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	return result
}

func copyMap(values map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func normalizeUintField(values map[string]interface{}, key string) {
	if raw, ok := values[key].(string); ok {
		var value uint64
		if _, err := fmt.Sscanf(raw, "%d", &value); err == nil {
			values[key] = value
		}
	}
}
