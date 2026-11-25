// Package dbtypes provides persistence types shared by platform database consumers.
package dbtypes

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

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
