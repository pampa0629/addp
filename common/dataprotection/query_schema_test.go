package dataprotection

import (
	"errors"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestProtectedComputedAndOpaqueOutputsHaveDifferentReasons(t *testing.T) {
	rules := testProjection(time.Now().UTC()).Rules
	for _, item := range []struct {
		source plugin.QueryOutputSource
		want   error
	}{
		{plugin.QueryOutputSource{OpaqueOutput: true}, ErrLineageUnverified},
		{plugin.QueryOutputSource{Bindings: []plugin.QueryOutputBinding{{SourcePath: []string{"userInfo", "phone"}, OutputPath: []string{"computed"}, Transformation: plugin.QueryOutputTransformationDerived}}}, ErrComputedOutputUnsupported},
	} {
		result := &plugin.QueryResult{Columns: []string{"computed"}, Rows: []map[string]any{{"computed": "private-sentinel"}}}
		if err := ProtectQueryResultSource(result, item.source, "preview", rules, SubjectReference{}); !errors.Is(err, item.want) || !errors.Is(err, ErrDenied) {
			t.Fatalf("wrong output refusal: %v", err)
		}
		if _, err := QueryOutputTransformsAt(item.source, "preview", rules, SubjectReference{}, time.Now().UTC()); !errors.Is(err, item.want) || !errors.Is(err, ErrDenied) {
			t.Fatalf("wrong transform refusal: %v", err)
		}
	}
}

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
