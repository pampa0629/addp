package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/lib/pq"
)

// JSONB 类型 - 用于 PostgreSQL JSONB 字段
type JSONB map[string]interface{}

// Value 实现 driver.Valuer 接口
func (j JSONB) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Scan 实现 sql.Scanner 接口
func (j *JSONB) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}

	result := make(map[string]interface{})
	if err := json.Unmarshal(bytes, &result); err != nil {
		return err
	}

	*j = result
	return nil
}

// GormDataType 指定 gorm 数据类型
func (JSONB) GormDataType() string {
	return "jsonb"
}

// StringArray 类型 - 用于 PostgreSQL TEXT[] 字段
type StringArray []string

// Value 实现 driver.Valuer 接口
func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	return pq.Array(a).Value()
}

// Scan 实现 sql.Scanner 接口
func (a *StringArray) Scan(value interface{}) error {
	if value == nil {
		*a = nil
		return nil
	}

	// 使用 pq.Array 来扫描
	arr := pq.StringArray{}
	if err := arr.Scan(value); err != nil {
		return fmt.Errorf("failed to scan StringArray: %w", err)
	}

	*a = StringArray(arr)
	return nil
}

// GormDataType 指定 gorm 数据类型
func (StringArray) GormDataType() string {
	return "text[]"
}
