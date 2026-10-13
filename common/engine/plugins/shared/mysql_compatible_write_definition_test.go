package shared

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared/writetest"
)

func TestMySQLCompatibleWriteDefinition(t *testing.T) {
	writetest.MySQLNonSpatialDefinition(t, MySQLCompatibleTableWriter{EngineType: "tidb", DefaultPort: 4000})
	writetest.PureDefinitionConstants(t, MySQLCompatibleTableWriter{EngineType: "tidb", DefaultPort: 4000})
}

type forbiddenConstantHook struct{ calls *int }

func (v forbiddenConstantHook) Value() (driver.Value, error) {
	*v.calls++
	panic("must not invoke Valuer")
}
func (v forbiddenConstantHook) MarshalJSON() ([]byte, error) {
	*v.calls++
	panic("must not invoke Marshaler")
}

func TestMySQLCompatibleDefinitionNeverInvokesConstantHooks(t *testing.T) {
	calls := 0
	w := MySQLCompatibleTableWriter{EngineType: "tidb", DefaultPort: 4000}
	for _, value := range []interface{}{forbiddenConstantHook{&calls}, map[string]interface{}{"nested": []interface{}{forbiddenConstantHook{&calls}}}} {
		report := w.CheckTableWriteDefinition(plugin.TableWriteDefinition{Fields: []plugin.TableWriteFieldDefinition{{Target: datatype.FieldInfo{Name: "value", Type: datatype.FieldTypeJSON}, Constant: &plugin.TableWriteConstant{Value: value}}}})
		last := report.Checks[len(report.Checks)-1]
		if last.Status != plugin.DefinitionUnknown || last.Code != "constant_encoding_not_checked" || calls != 0 {
			t.Fatalf("report=%+v calls=%d", report, calls)
		}
	}
}

func TestMySQLCompatibleInvalidDefinitionRejectedBeforeConnection(t *testing.T) {
	w := MySQLCompatibleTableWriter{EngineType: "tidb", DefaultPort: 4000, UpsertValueReference: MySQLCompatibleUpsertValueReferenceValuesFunction}
	fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true}, {Name: "value", Type: datatype.FieldTypeArray}}
	opts := plugin.TableUpsertOptions{Fields: fields, Keys: []string{"id"}}
	path := plugin.EngineCatalogPath{}
	batch := &plugin.BatchData{Rows: []map[string]interface{}{{"id": 1, "value": []interface{}{"a"}}}}
	errors := []error{}
	errors = append(errors, w.PrepareTableWrite(context.Background(), nil, path, plugin.TableWriteOptions{Fields: fields}))
	_, err := w.OpenTableWriteSession(context.Background(), nil, path, plugin.TableWriteSessionOptions{Fields: fields})
	errors = append(errors, err, w.PrepareTableUpsert(context.Background(), nil, path, opts), w.UpsertBatch(context.Background(), nil, path, batch, opts))
	for _, err := range errors {
		if err == nil || !strings.Contains(err.Error(), `column "value": field_definition_unsupported`) {
			t.Fatalf("reached path/connection validation: %v", err)
		}
	}
}
