package parquet

import (
	"bytes"
	"context"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	parquetgo "github.com/parquet-go/parquet-go"
)

func decimalParquetFixture(t *testing.T, precision, scale int, physical parquetgo.Type, values ...parquetgo.Value) []byte {
	t.Helper()
	schema := parquetgo.NewSchema("decimal_fixture", parquetgo.Group{
		"amount": parquetgo.Optional(parquetgo.Decimal(scale, precision, physical)),
	})
	var buffer bytes.Buffer
	writer := parquetgo.NewWriter(&buffer, schema, parquetgo.MaxRowsPerRowGroup(1))
	for _, value := range values {
		definition := 1
		if value.IsNull() {
			definition = 0
		}
		if _, err := writer.WriteRows([]parquetgo.Row{{value.Level(0, definition, 0)}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func decimalFixedValue(t *testing.T, encoded string) parquetgo.Value {
	t.Helper()
	value, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return parquetgo.FixedLenByteArrayValue(value)
}

func TestParquetDecimalReadEncodingsAndMetadata(t *testing.T) {
	ctx := context.Background()
	plugin := NewPlugin()
	for _, tc := range []struct {
		name             string
		precision, scale int
		physical         parquetgo.Type
		values           []parquetgo.Value
		want             []interface{}
	}{
		{"int32", 9, 2, parquetgo.Int32Type, []parquetgo.Value{parquetgo.Int32Value(125), parquetgo.Int32Value(-1250), parquetgo.Int32Value(0), parquetgo.NullValue()}, []interface{}{"1.25", "-12.50", "0.00", nil}},
		{"int64", 18, 4, parquetgo.Int64Type, []parquetgo.Value{parquetgo.Int64Value(9007199254740993), parquetgo.Int64Value(-1)}, []interface{}{"900719925474.0993", "-0.0001"}},
		{"fixed_spark", 38, 18, parquetgo.FixedLenByteArrayType(16), []parquetgo.Value{
			decimalFixedValue(t, "00000000000000056bc75e2d63100000"),
			decimalFixedValue(t, "ffffffffffffffffffffffffffffffff"),
			decimalFixedValue(t, "00000000000000000000000000000000"),
		}, []interface{}{"100.000000000000000000", "-0.000000000000000001", "0.000000000000000000"}},
		{"scale_zero", 9, 0, parquetgo.Int32Type, []parquetgo.Value{parquetgo.Int32Value(-123)}, []interface{}{"-123"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := decimalParquetFixture(t, tc.precision, tc.scale, tc.physical, tc.values...)
			description, err := plugin.DescribeTable(ctx, bytes.NewReader(data), nil)
			if err != nil {
				t.Fatal(err)
			}
			field := description.Table.Fields[0]
			if field.Type != datatype.FieldTypeDecimal || field.Precision != tc.precision || field.Scale != tc.scale || field.NativeType == "" {
				t.Errorf("decimal metadata = %#v", field)
			}
			reader, err := plugin.OpenTableReader(ctx, bytes.NewReader(data), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close(ctx)
			for index, want := range tc.want {
				// Independent row groups exercise both continuous batches and offset paging.
				for route, read := range map[string]func() ([]map[string]interface{}, error){
					"sample": func() ([]map[string]interface{}, error) {
						return plugin.SampleTable(ctx, bytes.NewReader(data), int64(index), 1, nil)
					},
					"stream": func() ([]map[string]interface{}, error) { return reader.ReadRows(ctx, 1) },
				} {
					rows, err := read()
					if err != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0]["amount"], want) {
						t.Errorf("%s row %d = %#v, %v; want %v", route, index, rows, err, want)
					}
				}
			}
		})
	}
}

func TestParquetDecimalScopeRejectsPrecisionOrScaleChanges(t *testing.T) {
	ctx := context.Background()
	for _, other := range []struct{ precision, scale int }{{11, 2}, {10, 3}} {
		reader := parquetMemoryContentReader{data: map[string][]byte{
			"dataset/part-0.parquet": decimalParquetFixture(t, 10, 2, parquetgo.Int64Type, parquetgo.Int64Value(125)),
			"dataset/part-1.parquet": decimalParquetFixture(t, other.precision, other.scale, parquetgo.Int64Type, parquetgo.Int64Value(125)),
		}}
		if _, err := NewPlugin().DescribeTableScope(ctx, reader, contentio.NewRef("dataset", contentio.RoleScope), nil); err == nil || !strings.Contains(err.Error(), "incompatible") {
			t.Errorf("scope accepted different Decimal(%d,%d): %v", other.precision, other.scale, err)
		}
	}
}

func TestParquetDecimalReadRejectsInvalidPrecisionScaleOrValue(t *testing.T) {
	for _, tc := range []struct{ precision, scale, unscaled int }{{0, 0, 1}, {10, 2, 1}, {9, -1, 1}, {9, 10, 1}, {2, 0, 100}} {
		data := decimalParquetFixture(t, tc.precision, tc.scale, parquetgo.Int32Type, parquetgo.Int32Value(int32(tc.unscaled)))
		if _, err := NewPlugin().SampleTable(context.Background(), bytes.NewReader(data), 0, 1, nil); err == nil {
			t.Errorf("accepted Decimal(%d,%d) with unscaled %d", tc.precision, tc.scale, tc.unscaled)
		}
	}
}

func TestParquetDecimalReadSurvivesFieldProjection(t *testing.T) {
	ctx := context.Background()
	var buffer bytes.Buffer
	schema := parquetgo.NewSchema("projected_decimal", parquetgo.Group{
		"a_label": parquetgo.String(),
		"amount":  parquetgo.Decimal(2, 9, parquetgo.Int32Type),
	})
	writer := parquetgo.NewWriter(&buffer, schema)
	if _, err := writer.WriteRows([]parquetgo.Row{{parquetgo.ByteArrayValue([]byte("row")).Level(0, 0, 0), parquetgo.Int32Value(125).Level(0, 0, 1)}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	options := format.DefaultParseOptions()
	options.FieldSelection = &format.FieldSelectionOptions{Include: []string{"amount"}}
	reader, err := NewPlugin().OpenTableReader(ctx, bytes.NewReader(buffer.Bytes()), options)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(ctx)
	rows, err := reader.ReadRows(ctx, 1)
	if err != nil || !reflect.DeepEqual(rows, []map[string]interface{}{{"amount": "1.25"}}) {
		t.Fatalf("projected Decimal rows = %#v, %v", rows, err)
	}
}

func TestParquetDecimalScopeSampleAndContinuousRead(t *testing.T) {
	ctx := context.Background()
	input := parquetMemoryContentReader{data: map[string][]byte{
		"dataset/part-0.parquet": decimalParquetFixture(t, 9, 2, parquetgo.Int32Type, parquetgo.Int32Value(125)),
		"dataset/part-1.parquet": decimalParquetFixture(t, 9, 2, parquetgo.Int32Type, parquetgo.Int32Value(-1250)),
	}}
	scope := contentio.NewRef("dataset", contentio.RoleScope)
	plugin := NewPlugin()
	want := []map[string]interface{}{{"amount": "1.25"}, {"amount": "-12.50"}}
	rows, err := plugin.SampleTableScope(ctx, input, scope, 0, 2, nil)
	if err != nil || !reflect.DeepEqual(rows, want) {
		t.Errorf("scope sample = %#v, %v", rows, err)
	}
	reader, err := plugin.OpenTableScopeReader(ctx, input, scope, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(ctx)
	rows, err = reader.ReadRows(ctx, 2)
	if err != nil || !reflect.DeepEqual(rows, want) {
		t.Errorf("scope continuous read = %#v, %v", rows, err)
	}
}
