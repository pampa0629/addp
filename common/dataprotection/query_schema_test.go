package dataprotection

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestSuppressedOutputRemovesResultFieldMetadata(t *testing.T) {
	for _, withRows := range []bool{false, true} {
		result := &plugin.QueryResult{
			Columns: []string{"visible", "hidden_alias"},
			Fields:  []datatype.FieldInfo{{Name: "visible", Type: datatype.FieldTypeInt}, {Name: "hidden_alias", Type: datatype.FieldTypeBigInt}},
		}
		if withRows {
			result.Rows = []map[string]interface{}{{"visible": int64(2), "hidden_alias": int64(9007199254740993)}}
		}
		source := plugin.QueryOutputSource{Bindings: []plugin.QueryOutputBinding{{SourcePath: []string{"secret"}, OutputPath: []string{"hidden_alias"}, Transformation: plugin.QueryOutputTransformationDirect}}}
		rules := []Rule{{Action: "query", Component: Component{Key: "secret", Path: []PathSegment{{Name: "secret", Container: "scalar"}}, ValueType: "bigint"}, Decision: Decision{Effect: EffectSuppress}}}
		if err := ProtectQueryResultSource(result, source, "query", rules, SubjectReference{}); err != nil {
			t.Fatal(err)
		}
		if len(result.Columns) != 1 || result.Columns[0] != "visible" || len(result.Fields) != 1 || result.Fields[0].Name != "visible" {
			t.Fatal("suppressed column metadata remains visible")
		}
		for _, row := range result.Rows {
			if _, exists := row["hidden_alias"]; exists {
				t.Fatal("suppressed value remains visible")
			}
		}
	}
}
