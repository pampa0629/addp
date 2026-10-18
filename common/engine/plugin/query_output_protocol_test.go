package plugin

import (
	"errors"
	"testing"

	"github.com/addp/common/datatype"
)

func TestSQLQueryOutputProtocolValidatesAllRowsBeforeChangingAny(t *testing.T) {
	for _, lateFlag := range []any{nil, "true", 1} {
		result := &QueryResult{
			Columns: []string{"phone", "flag"},
			Fields:  []datatype.FieldInfo{{Name: "phone", Type: datatype.FieldTypeString}, {Name: "flag", Type: datatype.FieldTypeBool}},
			Rows:    []map[string]any{{"phone": "first", "flag": true}, {"phone": "second", "flag": lateFlag}},
		}
		err := FinalizeSQLQueryOutput(result, []string{"phone"}, nil, []SQLQueryOutputInvalid{{Flag: "flag", Column: "phone"}}, func(field datatype.FieldInfo, value any) (bool, bool) {
			if field.Type != datatype.FieldTypeBool {
				return false, false
			}
			if value == nil {
				return false, true
			}
			flag, ok := value.(bool)
			return flag, ok
		})
		if !errors.Is(err, ErrQueryOutputTransformUnsupported) {
			t.Fatal("late invalid flag accepted")
		}
		if result.Rows[0]["phone"] != "first" || result.Rows[0]["flag"] != true || len(result.Columns) != 2 || len(result.Fields) != 2 {
			t.Fatal("failed protocol changed an earlier row or output metadata")
		}
	}
}
