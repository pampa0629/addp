package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// JSONMap is a custom JSON type for GORM that stores map[string]interface{} as JSONB in PostgreSQL
// This type is shared across all modules to ensure consistent JSON handling
type JSONMap map[string]interface{}

// Value implements driver.Valuer interface for database serialization
func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}

// Scan implements sql.Scanner interface for database deserialization
func (m *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*m = JSONMap{}
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to scan JSONMap: expected []byte, got %T", value)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(bytes, &data); err != nil {
		return fmt.Errorf("failed to unmarshal JSONMap: %w", err)
	}

	*m = JSONMap(data)
	return nil
}

// Get retrieves a value by key with optional default
func (m JSONMap) Get(key string, defaultValue interface{}) interface{} {
	if val, ok := m[key]; ok {
		return val
	}
	return defaultValue
}

// GetString retrieves a string value by key
func (m JSONMap) GetString(key string) (string, bool) {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str, true
		}
	}
	return "", false
}

// GetInt retrieves an int value by key
func (m JSONMap) GetInt(key string) (int, bool) {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case int:
			return v, true
		case float64:
			return int(v), true
		case int64:
			return int(v), true
		}
	}
	return 0, false
}

// GetBool retrieves a bool value by key
func (m JSONMap) GetBool(key string) (bool, bool) {
	if val, ok := m[key]; ok {
		if b, ok := val.(bool); ok {
			return b, true
		}
	}
	return false, false
}

// Set sets a value by key
func (m JSONMap) Set(key string, value interface{}) {
	m[key] = value
}

// Delete removes a key from the map
func (m JSONMap) Delete(key string) {
	delete(m, key)
}

// Clone creates a deep copy of the JSONMap
func (m JSONMap) Clone() JSONMap {
	if m == nil {
		return nil
	}
	clone := make(JSONMap, len(m))
	for k, v := range m {
		clone[k] = cloneValue(v)
	}
	return clone
}

// cloneValue recursively clones nested map/slice values
func cloneValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		clone := make(map[string]interface{}, len(v))
		for k, val := range v {
			clone[k] = cloneValue(val)
		}
		return clone
	case []interface{}:
		clone := make([]interface{}, len(v))
		for i, val := range v {
			clone[i] = cloneValue(val)
		}
		return clone
	default:
		return v
	}
}

// MergeWith merges another JSONMap into this one (overwrites existing keys)
func (m JSONMap) MergeWith(other JSONMap) {
	for k, v := range other {
		m[k] = v
	}
}

// MergePreserving merges another JSONMap but preserves existing keys
func (m JSONMap) MergePreserving(other JSONMap, sensitiveKeys []string) {
	for k, v := range other {
		// Skip if key exists and is in sensitive keys list
		if _, exists := m[k]; exists {
			skip := false
			for _, sensitive := range sensitiveKeys {
				if k == sensitive {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
		}
		m[k] = v
	}
}
