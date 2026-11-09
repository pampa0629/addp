package feather

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
)

type fakeRuntime struct {
	engineplugin.WorkflowRuntimeProvider
	change, lossy, nested bool
	calls                 []string
}

func (r *fakeRuntime) InvokeOperator(ctx context.Context, conn engineplugin.ConnectionInfo, name string, req engineplugin.OperatorInvokeRequest) (*engineplugin.OperatorInvokeResult, error) {
	r.calls = append(r.calls, name)
	if req.Params["protocol"] != Protocol || req.Params["access_plan"] == nil {
		return nil, fmt.Errorf("source binding absent")
	}
	payload := map[string]interface{}{"protocol": Protocol}
	if name == "feather.inspect" {
		field := map[string]interface{}{"name": "id", "type": "bigint", "native_type": "int64", "nullable": true}
		if r.nested {
			field["type"] = "json"
			field["native_type"] = "struct<x: int64>"
		}
		payload["describe"] = map[string]interface{}{"table": map[string]interface{}{"row_count": 3, "fields": []map[string]interface{}{field, {"name": "blob", "type": "bytes", "native_type": "binary", "nullable": true}}}, "format_info": map[string]interface{}{"source_version": "v1"}}
	} else {
		version := "v1"
		if r.change {
			version = "changed"
		}
		payload["source_version"] = version
		rows := []map[string]interface{}{{"id": "9223372036854775807", "blob": "AP8="}, {"id": nil, "blob": nil}, {"id": "-9223372036854775808", "blob": ""}}
		offset := req.Params["offset"].(int64)
		limit := req.Params["limit"].(int)
		selected := req.Params["columns"].([]string)
		out := []map[string]interface{}{}
		for i := offset; i < int64(len(rows)) && len(out) < limit; i++ {
			row := map[string]interface{}{}
			for _, column := range selected {
				row[column] = rows[i][column]
			}
			if r.lossy {
				row["id"] = float64(9007199254740993)
			}
			out = append(out, row)
		}
		payload["rows"] = out
	}
	return &engineplugin.OperatorInvokeResult{Result: map[string]interface{}{"result": payload}}, nil
}
func bound(t *testing.T, r *fakeRuntime) format.BoundTableProvider {
	t.Helper()
	plan, err := workflowaccess.NewSourcePlan(workflowaccess.Source{Kind: workflowaccess.KindFile, Format: "feather", Access: workflowaccess.Access{Method: workflowaccess.MethodMountedPath, Path: "/approved/source.feather"}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPlugin().BindTableProvider(r, engineplugin.ConnectionInfo{"host": "runtime"}, plan)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestRuntimePagesContinuousAndExactTypes(t *testing.T) {
	ctx := context.Background()
	p := bound(t, &fakeRuntime{})
	info, err := p.DescribeTable(ctx, nil)
	if err != nil || *info.Table.RowCount != 3 {
		t.Fatal(info, err)
	}
	tail, err := p.SampleTable(ctx, 2, 20, nil)
	if err != nil || len(tail) != 1 || tail[0]["id"] != int64(-9223372036854775808) || len(tail[0]["blob"].([]byte)) != 0 {
		t.Fatal(tail, err)
	}
	reader, err := p.OpenTableReader(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := reader.ReadRows(ctx, 2)
	if err != nil || rows[0]["id"] != int64(9223372036854775807) || rows[1]["id"] != nil {
		t.Fatal(rows, err)
	}
	if !reflect.DeepEqual(rows[0]["blob"], []byte{0, 255}) {
		t.Fatal(rows)
	}
	rows, err = reader.ReadRows(ctx, 20)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	rows, err = reader.ReadRows(ctx, 20)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	opts := format.DefaultParseOptions()
	opts.FieldSelection = &format.FieldSelectionOptions{Include: []string{"blob"}}
	selected, err := p.SampleTable(ctx, 0, 1, opts)
	if err != nil || len(selected[0]) != 1 {
		t.Fatal(selected, err)
	}
}
func TestVersionSchemaLossAndCancellationRefused(t *testing.T) {
	ctx := context.Background()
	for _, r := range []*fakeRuntime{{change: true}, {lossy: true}, {nested: true}} {
		if _, err := bound(t, r).SampleTable(ctx, 0, 1, nil); err == nil {
			t.Fatal("invalid batch admitted")
		}
	}
	opts := format.DefaultParseOptions()
	opts.ExtraParams = map[string]interface{}{"source_version": "old"}
	if _, err := bound(t, &fakeRuntime{}).DescribeTable(ctx, opts); err != format.ErrTableSourceChanged {
		t.Fatal(err)
	}
	opts.FieldSelection = &format.FieldSelectionOptions{Include: []string{"blob"}}
	opts.ExtraParams = nil
	if _, err := bound(t, &fakeRuntime{nested: true}).DescribeTable(ctx, opts); err == nil {
		t.Fatal("unsupported schema hidden by projection")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := bound(t, &fakeRuntime{}).DescribeTable(cancelled, nil); err == nil {
		t.Fatal("cancel ignored")
	}
	if _, err := bound(t, &fakeRuntime{}).SampleTable(ctx, -1, 1, nil); err == nil {
		t.Fatal("negative offset")
	}
}

func TestDateAndDecimalWireContract(t *testing.T) {
	date := datatype.FieldInfo{Type: datatype.FieldTypeDate, Nullable: true}
	decimal := datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 6, Scale: 3, Nullable: true}
	for _, tc := range []struct {
		field datatype.FieldInfo
		value any
		valid bool
	}{
		{date, "1969-12-31", true},
		{date, "2000-02-29", true},
		{date, nil, true},
		{date, "1900-02-29", false},
		{date, "0000-01-01", false},
		{decimal, "-123.450", true},
		{decimal, "0.001", true},
		{decimal, "1.23", false},
		{decimal, "1000.000", false},
		{decimal, "1e-3", false},
		{decimal, 1.23, false},
	} {
		got, err := decodeValue(tc.value, tc.field)
		if tc.valid && (err != nil || got != tc.value) || !tc.valid && err == nil {
			t.Fatalf("%s %v: got %v, %v", tc.field.Type, tc.value, got, err)
		}
	}
}
