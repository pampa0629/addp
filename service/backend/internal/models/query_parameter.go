package models

import "github.com/addp/common/datatype"

// QueryServiceNamedParameter 是 SQL 模式查询服务发布的强类型标量输入。
type QueryServiceNamedParameter struct {
	Name        string             `json:"name"`
	Type        datatype.FieldType `json:"type"`
	Required    bool               `json:"required"`
	Description string             `json:"description,omitempty"`
	Default     interface{}        `json:"default,omitempty" swaggertype:"object"`
}
