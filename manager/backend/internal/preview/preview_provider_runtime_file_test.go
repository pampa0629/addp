package preview

import (
	"context"
	"fmt"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"testing"
)

type boundFileTableStub struct {
	format.BoundTableProvider
	changed bool
}

func (*boundFileTableStub) Format() format.FormatType { return format.FormatORC }
func (s *boundFileTableStub) DescribeTable(_ context.Context, opts *format.ParseOptions) (*format.TableDescribeResult, error) {
	if opts == nil || opts.ExtraParams["source_version"] != "v1" || s.changed {
		return nil, fmt.Errorf("source changed")
	}
	n := int64(23)
	return &format.TableDescribeResult{Table: &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt, NativeType: "bigint", Nullable: true}}, RowCount: &n}}, nil
}
func (*boundFileTableStub) SampleTable(_ context.Context, offset, limit int64, opts *format.ParseOptions) ([]map[string]interface{}, error) {
	rows := []map[string]interface{}{}
	for i := offset; i < 23 && i < offset+limit; i++ {
		rows = append(rows, map[string]interface{}{"id": int64(9007199254740993 + i)})
	}
	return rows, nil
}
func TestRuntimeFilePreviewUsesScannedVersionAndTrueTail(t *testing.T) {
	req := &PreviewRequest{Page: 2, PageSize: 20, RuntimeTableProvider: &boundFileTableStub{}, Attributes: map[string]interface{}{"format_info": map[string]interface{}{"orc": map[string]interface{}{"source_version": "v1"}}}}
	result, err := NewFileTablePreviewProvider().Preview(context.Background(), req)
	if err != nil || len(result.Rows) != 3 || result.HasMore || result.Total == nil || *result.Total != 23 || result.Rows[0]["id"] != int64(9007199254741013) {
		t.Fatalf("preview: %#v %v", result, err)
	}
	req.RuntimeTableProvider = &boundFileTableStub{changed: true}
	if _, err = NewFileTablePreviewProvider().Preview(context.Background(), req); err == nil {
		t.Fatal("changed source accepted")
	}
	req.RuntimeTableProvider = &boundFileTableStub{}
	req.Attributes = nil
	if _, err = NewFileTablePreviewProvider().Preview(context.Background(), req); err == nil {
		t.Fatal("unscanned source accepted")
	}
}
