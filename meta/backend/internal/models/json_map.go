package models

import (
	"database/sql/driver"
	"encoding/json"
)

// JSONMap 提供基础的 JSONB 映射能力
type JSONMap map[string]interface{}

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}

func (m *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*m = JSONMap{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	var data map[string]interface{}
	if err := json.Unmarshal(bytes, &data); err != nil {
		return err
	}
	*m = JSONMap(data)
	return nil
}
