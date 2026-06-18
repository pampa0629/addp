package events

import (
	"encoding/json"
	"fmt"
)

func ParseCleanupRequest(values map[string]interface{}) (CleanupRequestEvent, error) {
	normalized := copyMap(values)

	if modulesStr, ok := normalized["expected_modules"].(string); ok && modulesStr != "" {
		var modules []string
		if err := json.Unmarshal([]byte(modulesStr), &modules); err == nil {
			normalized["expected_modules"] = modules
		}
	}
	if contextStr, ok := normalized["context"].(string); ok && contextStr != "" {
		var eventContext map[string]interface{}
		if err := json.Unmarshal([]byte(contextStr), &eventContext); err == nil {
			normalized["context"] = eventContext
		}
	}

	normalizeUintField(normalized, "tenant_id")
	normalizeUintField(normalized, "requested_by")

	var event CleanupRequestEvent
	eventJSON, _ := json.Marshal(normalized)
	if err := json.Unmarshal(eventJSON, &event); err != nil {
		return CleanupRequestEvent{}, err
	}
	return event, nil
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
