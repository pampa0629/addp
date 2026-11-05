package mysql

import (
	"encoding/binary"
	"fmt"

	"github.com/addp/common/datatype"
)

// DecodeSQLQueryValue consumes MySQL's native geometry, never a sampled BLOB.
// Both ordinary queries and streaming sessions use this exact decoder.
func (p *MySQLPlugin) DecodeSQLQueryValue(field datatype.FieldInfo, value interface{}) (interface{}, error) {
	if field.Type != datatype.FieldTypeGeometry || value == nil {
		return value, nil
	}
	raw, ok := value.([]byte)
	if !ok || len(raw) < 9 {
		return nil, fmt.Errorf("invalid MySQL query geometry")
	}
	srid := int(binary.LittleEndian.Uint32(raw[:4]))
	return mysqlWKBToEWKB(raw[4:], srid)
}
