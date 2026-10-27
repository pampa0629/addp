package orc

import (
	"context"
	"fmt"
	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
	"testing"
)

type fakeRuntime struct {
	engineplugin.WorkflowRuntimeProvider
	calls     []string
	version   string
	change    bool
	nested    bool
	malformed bool
}

func (r *fakeRuntime) InvokeOperator(ctx context.Context, conn engineplugin.ConnectionInfo, name string, req engineplugin.OperatorInvokeRequest) (*engineplugin.OperatorInvokeResult, error) {
	r.calls = append(r.calls, name)
	if req.Params["protocol"] != Protocol || req.Params["access_plan"] == nil {
		return nil, fmt.Errorf("missing exact source plan")
	}
	payload := map[string]interface{}{"protocol": Protocol}
	if name == "orc.inspect" {
		payload["describe"] = map[string]interface{}{"table": map[string]interface{}{"row_count": 3, "fields": []map[string]interface{}{{"name": "id", "type": "bigint", "native_type": "bigint", "nullable": true}, {"name": "blob", "type": "bytes", "native_type": "binary", "nullable": true}}}, "format_info": map[string]interface{}{"source_version": r.version}}
		if r.nested {
			payload["describe"].(map[string]interface{})["table"].(map[string]interface{})["fields"] = nestedFields()
		}
	} else if name == "orc.read_batch" {
		v := r.version
		if r.change {
			v = "changed"
		}
		payload["source_version"] = v
		offset := req.Params["offset"].(int64)
		limit := req.Params["limit"].(int)
		if req.Params["source_version"] != r.version {
			return nil, fmt.Errorf("stale source")
		}
		rows := []map[string]interface{}{}
		all := []map[string]interface{}{{"id": "9223372036854775807", "blob": "AP8="}, {"id": nil, "blob": nil}, {"id": "-9223372036854775808", "blob": ""}}
		if r.nested {
			all = []map[string]interface{}{
				{"obj": map[string]interface{}{"id": "9007199254740993", "items": []interface{}{"AP8=", nil, ""}}},
				{"obj": nil},
				{"obj": map[string]interface{}{"id": nil, "items": []interface{}{}}},
			}
		}
		if r.nested {
			for i, row := range all {
				row["plain"] = float64(i)
			}
		}
		for i := offset; i < int64(len(all)) && len(rows) < limit; i++ {
			rows = append(rows, all[i])
		}
		if r.malformed && len(rows) > 0 {
			rows[0]["id"] = float64(9223372036854775807)
		}
		payload["rows"] = rows
	} else {
		return nil, fmt.Errorf("unexpected operator")
	}
	return &engineplugin.OperatorInvokeResult{Result: map[string]interface{}{"result": payload}}, nil
}
func bound(t *testing.T, r *fakeRuntime) format.BoundTableProvider {
	t.Helper()
	plan, err := workflowaccess.NewSourcePlan(workflowaccess.Source{Kind: workflowaccess.KindFile, Format: "orc", Access: workflowaccess.Access{Method: workflowaccess.MethodMountedPath, Path: "/approved/source.orc"}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPlugin().BindTableProvider(r, engineplugin.ConnectionInfo{"host": "runtime"}, plan)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestRuntimeReadWindowsAndExactTypes(t *testing.T) {
	ctx := context.Background()
	r := &fakeRuntime{version: "v1"}
	p := bound(t, r)
	info, err := p.DescribeTable(ctx, nil)
	if err != nil || *info.Table.RowCount != 3 {
		t.Fatalf("describe: %v %#v", err, info)
	}
	rows, err := p.SampleTable(ctx, 2, 20, nil)
	if err != nil || len(rows) != 1 || rows[0]["id"] != int64(-9223372036854775808) || len(rows[0]["blob"].([]byte)) != 0 {
		t.Fatalf("tail: %#v %v", rows, err)
	}
	reader, err := p.OpenTableReader(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reader.Fields()[0].Type != datatype.FieldTypeBigInt {
		t.Fatal("field identity")
	}
	rows, err = reader.ReadRows(ctx, 2)
	if err != nil || rows[0]["id"] != int64(9223372036854775807) || rows[1]["id"] != nil {
		t.Fatalf("first: %#v %v", rows, err)
	}
	if b := rows[0]["blob"].([]byte); len(b) != 2 || b[1] != 255 {
		t.Fatal("binary changed")
	}
	rows, err = reader.ReadRows(ctx, 2)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	rows, err = reader.ReadRows(ctx, 2)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	if err = reader.Close(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestRuntimeRejectsChangedVersionAndLossyNumbers(t *testing.T) {
	for _, change := range []bool{true, false} {
		r := &fakeRuntime{version: "v1", change: change, malformed: !change}
		reader, err := bound(t, r).OpenTableReader(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = reader.ReadRows(context.Background(), 2); err == nil {
			t.Fatal("unsafe runtime batch accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := bound(t, &fakeRuntime{version: "v1"}).DescribeTable(ctx, nil); err == nil {
		t.Fatal("cancel ignored")
	}
}

func TestDescribeDoesNotRebindScannedVersion(t *testing.T) {
	p := bound(t, &fakeRuntime{version: "new"})
	options := format.DefaultParseOptions()
	options.ExtraParams = map[string]interface{}{"source_version": "old"}
	if _, err := p.DescribeTable(context.Background(), options); err != format.ErrTableSourceChanged {
		t.Fatalf("source version was rebound: %v", err)
	}
}

func nestedFields() []map[string]interface{} {
	return []map[string]interface{}{{"name": "plain", "type": "int", "nullable": true}, {"name": "obj", "type": "json", "nullable": true, "fields": []map[string]interface{}{
		{"name": "id", "type": "bigint", "nullable": true},
		{"name": "items", "type": "array", "nullable": true, "element_type": "bytes", "element": map[string]interface{}{"type": "bytes", "nullable": true}},
	}}}
}
func TestNestedPreviewPreservesSchemaValuesAndRejectsBatch(t *testing.T) {
	ctx := context.Background()
	runtime := &fakeRuntime{version: "v1", nested: true}
	p := bound(t, runtime)
	info, err := p.DescribeTable(ctx, nil)
	if err != nil || info.Table.Fields[1].Fields[1].Element.Type != datatype.FieldTypeBytes {
		t.Fatalf("nested facts: %#v %v", info, err)
	}
	rows, err := p.SampleTable(ctx, 0, 20, nil)
	if err != nil {
		t.Fatal(err)
	}
	object := rows[0]["obj"].(map[string]interface{})
	if object["id"] != int64(9007199254740993) || rows[1]["obj"] != nil {
		t.Fatal(rows)
	}
	items := object["items"].([]interface{})
	if len(items[0].([]byte)) != 2 || items[1] != nil || len(items[2].([]byte)) != 0 {
		t.Fatal(items)
	}
	public, err := datatype.RowsForJSON(info.Table.Fields, rows)
	if err != nil || public[0]["obj"].(map[string]interface{})["id"] != "9007199254740993" {
		t.Fatal(public, err)
	}
	projected := format.DefaultParseOptions()
	projected.FieldSelection = &format.FieldSelectionOptions{Include: []string{"plain"}}
	if _, err := p.OpenTableReader(ctx, projected); err == nil {
		t.Fatal("scalar projection bypassed nested batch boundary")
	}
	runtime.calls = nil
	if _, err := p.OpenTableReader(ctx, nil); err == nil {
		t.Fatal("nested batch admitted")
	}
	if len(runtime.calls) != 1 || runtime.calls[0] != "orc.inspect" {
		t.Fatal("batch read before refusal", runtime.calls)
	}
}
func TestNestedDecoderRejectsMissingMemberAndLossyInteger(t *testing.T) {
	field := datatype.FieldInfo{Type: datatype.FieldTypeJSON, Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}}}
	for _, value := range []interface{}{map[string]interface{}{}, map[string]interface{}{"id": float64(9007199254740993)}, []interface{}{}} {
		if _, err := decodeValue(value, field, 0); err == nil {
			t.Fatal("unsafe nested wire accepted")
		}
	}
}
