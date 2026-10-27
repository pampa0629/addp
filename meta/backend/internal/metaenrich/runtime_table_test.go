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

type tableInspectorStub struct{ fail bool }

func (s tableInspectorStub) InspectTable(_ context.Context, _ *commonModels.Engine, _ uint, path, kind string) (*format.TableDescribeResult, error) {
	if s.fail {
		return nil, errors.New("rejected")
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
