package dataprotection

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"strings"
	"testing"
)

func TestCloneQueryResultPreservesIndependentBinaryValues(t *testing.T) {
	raw := []byte{0, 255, 128}
	result := &plugin.QueryResult{Rows: []map[string]interface{}{{"shape": raw, "masked": "***", "empty": []byte{}, "nested": map[string]interface{}{"bytes": raw}}}}
	clone, err := CloneQueryResult(result)
	if err != nil {
		t.Fatal(err)
	}
	copied, ok := clone.Rows[0]["shape"].([]byte)
	if !ok || !bytes.Equal(raw, copied) {
		t.Fatal("snapshot erased byte type")
	}
	copied[0] = 9
	if raw[0] != 0 || clone.Rows[0]["masked"] != "***" {
		t.Fatal("snapshot shared bytes or restored masked value")
	}
	if empty, ok := clone.Rows[0]["empty"].([]byte); !ok || empty == nil || len(empty) != 0 {
		t.Fatal("empty bytes became NULL")
	}
	if nested, ok := clone.Rows[0]["nested"].(map[string]interface{})["bytes"].([]byte); !ok || !bytes.Equal(nested, raw) {
		t.Fatal("nested bytes lost")
	}
}

func TestGeometryProjectionRunsOnlyAfterQueryProtection(t *testing.T) {
	geometry, _ := hex.DecodeString("0101000020e6100000000000000000f03f0000000000000040")
	result := &plugin.QueryResult{Columns: []string{"shape_alias"}, Fields: []datatype.FieldInfo{{Name: "shape_alias", Type: datatype.FieldTypeGeometry}}, Rows: []map[string]interface{}{{"shape_alias": geometry}}}
	source := plugin.QueryOutputSource{IdentityOutput: true}
	rules := []Rule{{Action: "query", Component: Component{Key: "shape_alias", Path: []PathSegment{{Name: "shape_alias", Container: "scalar"}}, ValueType: "geometry"}, Decision: Decision{Effect: EffectSuppress}}}
	if err := ProtectQueryResultSource(result, source, "query", rules, SubjectReference{Type: "user", ID: "1"}); err != nil {
		t.Fatal(err)
	}
	wire, err := datatype.QueryRowsForJSON(result.Fields, result.Rows)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(wire)
	if len(result.Fields) != 0 || len(result.Columns) != 0 || strings.Contains(string(encoded), "POINT") || strings.Contains(string(encoded), "4326") || strings.Contains(string(encoded), "shape_alias") {
		t.Fatal("suppressed geometry leaked through presentation")
	}
}
