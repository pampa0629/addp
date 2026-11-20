package iam

import (
	"context"
	"database/sql/driver"
	"encoding/json"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DBStringList maps ordered IAM strings to PostgreSQL arrays or MySQL JSON arrays.
type DBStringList []string

func (list *DBStringList) Scan(value any) error {
	var data []byte
	switch value := value.(type) {
	case []byte:
		data = value
	case string:
		data = []byte(value)
	}
	if len(data) > 0 && data[0] == '[' {
		return json.Unmarshal(data, list)
	}
	return (*pq.StringArray)(list).Scan(value)
}

func (list DBStringList) Value() (driver.Value, error) {
	return pq.StringArray(list).Value()
}

func (DBStringList) GormDataType() string { return "json" }

func (list DBStringList) GormValue(_ context.Context, db *gorm.DB) clause.Expr {
	if db.Dialector.Name() == "mysql" && list != nil {
		encoded, _ := json.Marshal(list)
		return gorm.Expr("?", string(encoded))
	}
	return gorm.Expr("?", pq.StringArray(list))
}
