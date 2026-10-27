package executor

import (
	"context"
	"fmt"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"testing"
)

type boundTableStub struct {
	format.BoundTableProvider
	closed    bool
	offset    int
	wrongType bool
}

func (*boundTableStub) Format() format.FormatType { return format.FormatORC }
func (s *boundTableStub) OpenTableReader(_ context.Context, _ *format.ParseOptions) (format.TableReader, error) {
	return &boundRowsStub{s}, nil
}

type boundRowsStub struct{ s *boundTableStub }

func (r *boundRowsStub) Fields() []datatype.FieldInfo {
	kind := datatype.FieldTypeBigInt
	if r.s.wrongType {
		kind = datatype.FieldTypeString
	}
	return []datatype.FieldInfo{{Name: "id", Type: kind}}
}
func (r *boundRowsStub) ReadRows(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows := []map[string]interface{}{}
	for r.s.offset < 3 && len(rows) < limit {
		rows = append(rows, map[string]interface{}{"id": int64(9007199254740993 + r.s.offset)})
		r.s.offset++
	}
	return rows, nil
}
func (r *boundRowsStub) Close(context.Context) error { r.s.closed = true; return nil }
func TestRuntimeSingleFileBatchSourceUsesBoundReader(t *testing.T) {
	ctx := context.Background()
	bound := &boundTableStub{}
	exec := &TableTransferExecutor{SourceRuntimeTableProvider: bound}
	plan := TableSourcePlan{Kind: TableEndpointEncoded, Layout: format.LayoutSingle, Format: format.FormatORC, TableInfo: &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}}}}
	source, err := exec.openSource(plan)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := source.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{2, 1, 0} {
		batch, err := reader.ReadBatch(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Rows) != count {
			t.Fatal(fmt.Sprintf("rows %#v", batch.Rows))
		}
	}
	if err = reader.Close(ctx); err != nil || !bound.closed {
		t.Fatal("reader cleanup missing")
	}
	bound.wrongType = true
	source, err = exec.openSource(plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = source.Open(ctx); err == nil {
		t.Fatal("stale declared type accepted")
	}
	plan.Layout = format.LayoutWhole
	if _, err = exec.openSource(plan); err == nil {
		t.Fatal("ORC whole accepted")
	}
}
