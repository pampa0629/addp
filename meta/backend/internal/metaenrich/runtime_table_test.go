package metaenrich

import (
	"context"
	"errors"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
	"testing"
)

type tableInspectorStub struct {
	fail   bool
	nested bool
}

func (s tableInspectorStub) InspectTable(_ context.Context, _ *commonModels.Engine, _ uint, path, kind string) (*format.TableDescribeResult, error) {
	if s.fail {
		return nil, errors.New("rejected")
	}
	if s.nested {
		element := datatype.FieldInfo{Type: datatype.FieldTypeBigInt, Nullable: true}
		n := int64(43)
		return &format.TableDescribeResult{Table: &datatype.TableInfo{RowCount: &n, Fields: []datatype.FieldInfo{{Name: "items", Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeBigInt, Nullable: true, Element: &element}}}, FormatInfo: map[string]interface{}{"source_version": "fixed", "validation": "footer"}}, nil
	}
	n := int64(3)
	return &format.TableDescribeResult{Table: &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt, NativeType: "bigint", Nullable: true}}, RowCount: &n}, FormatInfo: map[string]interface{}{"source_version": "fixed", "validation": "footer"}}, nil
}
func TestRuntimeTableEnrichmentPersistsFactsAndRejectsFailure(t *testing.T) {
	item := &metaitem.DetectedItem{}
	item.Layout = format.LayoutSingle
	item.DataType = datatype.Table
	item.Format = "orc"
	attrs := models.JSONMap{}
	handled, err := EnrichRuntimeTableItem(context.Background(), attrs, tableInspectorStub{}, &commonModels.Engine{ID: 14}, 1, item, "exact.orc")
	if err != nil || !handled || len(item.Fields) != 1 || attrs["type_info"] == nil || attrs["format_info"] == nil {
		t.Fatalf("runtime table enrich: %v %v %#v", handled, err, attrs)
	}
	for _, stub := range []RuntimeTableInspector{nil, tableInspectorStub{fail: true}} {
		if handled, err := EnrichRuntimeTableItem(context.Background(), models.JSONMap{}, stub, nil, 1, item, "exact.orc"); !handled || err == nil {
			t.Fatal("runtime inspection failure was hidden")
		}
	}
}

func TestRuntimeTableEnrichmentRetainsDeclaredNestedSchema(t *testing.T) {
	item := &metaitem.DetectedItem{}
	item.Layout = format.LayoutSingle
	item.DataType = datatype.Table
	item.Format = "orc"
	attrs := models.JSONMap{}
	_, err := EnrichRuntimeTableItem(context.Background(), attrs, tableInspectorStub{nested: true}, &commonModels.Engine{ID: 14}, 1, item, "nested.orc")
	if err != nil {
		t.Fatal(err)
	}
	if len(item.Fields) != 1 || item.Fields[0].Element == nil || item.Fields[0].Element.Type != datatype.FieldTypeBigInt {
		t.Fatal("nested detection facts lost")
	}
	raw := attrs["type_info"].(map[string]interface{})["table"].(map[string]interface{})
	restored := datatype.TableInfoFromPayload(raw, "")
	if restored.Fields[0].Element.Type != datatype.FieldTypeBigInt {
		t.Fatal("nested metadata lost")
	}
}
