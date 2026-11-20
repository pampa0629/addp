package iam

import (
	"database/sql/driver"
	"encoding/json"

	"github.com/lib/pq"
)

// DBStringList reads ordered IAM strings from PostgreSQL arrays or MySQL JSON arrays.
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
