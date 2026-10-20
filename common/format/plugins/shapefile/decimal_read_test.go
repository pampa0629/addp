package shapefile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/jonas-p/go-shp"
)

func TestShapefileDecimalReadsPreserveExactText(t *testing.T) {
	values := []string{"1234567.123456789012345", "-123456.123456789012345", "0.000000000000001", "42", "9007199254740993.125", "-0.000000000000000", ""}
	base := createDecimalReadFixture(t, values)
	for _, indexed := range []bool{false, true} {
		name := "sequential"
		if indexed {
			name = "range"
		}
		t.Run(name, func(t *testing.T) {
			rr := newLocalRefReader(base)
			var reader contentio.Reader = rr
			if !indexed {
				reader = struct{ contentio.Reader }{rr}
			}
			p := NewPlugin(nil)
			info, err := p.DescribeMultiTable(context.Background(), reader, rr.refs(), nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, field := range info.Table.Fields {
				if field.Name == "AMOUNT" && (field.Type != datatype.FieldTypeDecimal || field.Precision != 24 || field.Scale != 15 || !field.Nullable) {
					t.Fatalf("decimal declaration changed: %#v", field)
				}
			}
			check := func(rows []map[string]interface{}, start int) {
				t.Helper()
				encoded, err := datatype.RowsForJSON(info.Table.Fields, rows)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := json.Marshal(encoded)
				if err != nil {
					t.Fatal(err)
				}
				var decoded []map[string]interface{}
				if err := json.Unmarshal(raw, &decoded); err != nil {
					t.Fatal(err)
				}
				for i, row := range rows {
					want := interface{}(values[start+i])
					if values[start+i] == "" {
						want = nil
					}
					if row["AMOUNT"] != want || decoded[i]["AMOUNT"] != want {
						t.Fatalf("precision/null lost at row %d: native=%T(%v) JSON=%v want=%v", start+i, row["AMOUNT"], row["AMOUNT"], decoded[i]["AMOUNT"], want)
					}
					if row["COUNT"] != int64(42) || row["APPROX"] != float64(1.25) {
						t.Fatalf("integer/float mapping changed: %#v", row)
					}
				}
			}
			var sampled []map[string]interface{}
			for offset := 0; offset < len(values); offset += 2 {
				rows, err := p.SampleMultiTable(context.Background(), reader, rr.refs(), int64(offset), 2, nil)
				if err != nil {
					t.Fatal(err)
				}
				if len(rows) != min(2, len(values)-offset) {
					t.Fatalf("missing page at %d: %#v", offset, rows)
				}
				check(rows, offset)
				sampled = append(sampled, rows...)
			}
			continuous, err := p.OpenMultiTableReader(context.Background(), reader, rr.refs(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer continuous.Close(context.Background())
			var streamed []map[string]interface{}
			for {
				rows, err := continuous.ReadRows(context.Background(), 2)
				if err != nil {
					t.Fatal(err)
				}
				if len(rows) == 0 {
					break
				}
				check(rows, len(streamed))
				streamed = append(streamed, rows...)
			}
			if len(streamed) != len(values) || !reflect.DeepEqual(sampled, streamed) {
				t.Fatalf("sample/stream rows differ: %#v / %#v", sampled, streamed)
			}
			if indexed && rr.rangeReads == 0 {
				t.Fatal("range path not exercised")
			}
			if !indexed && rr.rangeReads != 0 {
				t.Fatal("sequential path used ranges")
			}
		})
	}
}

func createDecimalReadFixture(t *testing.T, values []string) string {
	t.Helper()
	base := filepath.Join(t.TempDir(), "exact")
	writer, err := shp.Create(base+extSHP, shp.POINT)
	if err != nil {
		t.Fatal(err)
	}
	decimal := shp.NumberField("AMOUNT", 24)
	decimal.Precision = 15
	if err := writer.SetFields([]shp.Field{decimal, shp.NumberField("COUNT", 9), shp.FloatField("APPROX", 13, 6)}); err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		index := int(writer.Write(&shp.Point{X: 1, Y: 2}))
		// go-shp leaves unwritten bytes as NUL; DBF numeric fields require space padding.
		for field, value := range []string{fmt.Sprintf("%24s", value), fmt.Sprintf("%9s", "42"), fmt.Sprintf("%13s", "1.250000")} {
			if err := writer.WriteAttribute(index, field, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	writer.Close()
	normalizeGoShpDBFPath(t, base)
	if err := os.WriteFile(base+extCPG, []byte("UTF-8"), 0600); err != nil {
		t.Fatal(err)
	}
	return base
}
