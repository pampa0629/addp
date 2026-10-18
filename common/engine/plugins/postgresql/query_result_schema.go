package postgresql

import (
	"database/sql"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	pgmapping "github.com/addp/common/format/mappers/postgresql"
)

// SQLResultField maps PostgreSQL protocol result metadata, shared by the
// explicitly certified protocol providers. lib/pq and gaussdb-go decode absent
// NUMERIC typmod (-1) as precision=65535, scale=65531; those are sentinel bits,
// not a declared precision. An unbounded numeric expression stays decimal with
// unspecified precision instead of publishing those bits as schema facts.
func SQLResultField(column *sql.ColumnType) datatype.FieldInfo {
	field := plugin.SQLResultFieldMapper((&pgmapping.TypeMapper{}).ToCommon)(column)
	if strings.EqualFold(field.NativeType, "NUMERIC") && field.Precision == 65535 && field.Scale == 65531 {
		field.Precision, field.Scale = 0, 0
	}
	return field
}
