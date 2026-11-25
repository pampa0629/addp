package models

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// StringArray uses native text arrays on PostgreSQL and JSON arrays on MySQL.
type StringArray []string

func (a StringArray) GormValue(_ context.Context, db *gorm.DB) clause.Expr {
	var value driver.Valuer = pq.StringArray(a)
	if db.Dialector.Name() == "mysql" && a != nil {
		data, err := json.Marshal(a)
		if err != nil {
			db.AddError(err)
		}
		return clause.Expr{SQL: "?", Vars: []any{string(data)}}
	}
	return clause.Expr{SQL: "?", Vars: []any{value}}
}

func (a *StringArray) Scan(value any) error {
	if value == nil {
		*a = nil
		return nil
	}
	var data []byte
	switch v := value.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("cannot scan string array from %T", value)
	}
	if len(data) > 0 && data[0] == '[' {
		return json.Unmarshal(data, a)
	}
	return (*pq.StringArray)(a).Scan(data)
}

func (StringArray) GormDataType() string {
	return "text[]"
}
