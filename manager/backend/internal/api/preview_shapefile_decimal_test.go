package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	shapefileformat "github.com/addp/common/format/plugins/shapefile"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
	"github.com/gin-gonic/gin"
	"github.com/jonas-p/go-shp"
)

func TestShapefileDecimalSourceSerializesThroughPreviewBoundary(t *testing.T) {
	values := []string{"438545778.11722248792648", "9007199254740993.125", ""}
	base := filepath.Join(t.TempDir(), "source")
	writer, err := shp.Create(base+".shp", shp.POINT)
	if err != nil {
		t.Fatal(err)
	}
	field := shp.NumberField("AMOUNT", 24)
	field.Precision = 15
	if err := writer.SetFields([]shp.Field{field}); err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		row := int(writer.Write(&shp.Point{X: 1, Y: 2}))
		if err := writer.WriteAttribute(row, 0, fmt.Sprintf("%24s", value)); err != nil {
			t.Fatal(err)
		}
	}
	writer.Close()
	if _, err := os.Stat(base + "dbf"); err == nil {
		if err := os.Rename(base+"dbf", base+".dbf"); err != nil {
			t.Fatal(err)
		}
	}
	reader := shapefilePreviewSource{base: base}
	p := shapefileformat.NewPlugin(nil)
	refs := format.SameBasenameRelatedRefs(base+".shp", p.RelatedRefSpecs())
	info, err := p.DescribeMultiTable(context.Background(), reader, refs, nil)
	if err != nil {
		t.Fatal(err)
	}
	for offset := 0; offset < len(values); offset += 2 {
		rows, err := p.SampleMultiTable(context.Background(), reader, refs, int64(offset), 2, nil)
		if err != nil {
			t.Fatal(err)
		}
		table := &models.TablePreview{Mode: preview.PreviewModeTable, Fields: info.Table.Fields, Rows: rows,
			ColumnMetadata: []models.ColumnMetadata{{ColumnName: "AMOUNT", Type: "decimal"}}}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/preview", nil)
		writePreviewResult(c, &preview.PreviewResult{Data: table}, nil, dataprotection.SubjectReference{}, nil)
		if w.Code != 200 {
			t.Fatalf("Shapefile preview JSON rejected source decimal: %d %s", w.Code, w.Body.String())
		}
		var response struct {
			Data struct {
				Rows     []map[string]interface{} `json:"rows"`
				Metadata []models.ColumnMetadata  `json:"column_metadata"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Data.Rows) != min(2, len(values)-offset) || len(response.Data.Metadata) != 1 || response.Data.Metadata[0].Type != "decimal" {
			t.Fatalf("preview structure changed: %s", w.Body.String())
		}
		for i, row := range response.Data.Rows {
			want := interface{}(values[offset+i])
			if want == "" {
				want = nil
			}
			if row["AMOUNT"] != want || rows[i]["AMOUNT"] != want {
				t.Fatalf("decimal/null changed: public=%v native=%v want=%v", row["AMOUNT"], rows[i]["AMOUNT"], want)
			}
		}
		for _, f := range table.Fields {
			if f.Name == "AMOUNT" && (f.Type != datatype.FieldTypeDecimal || f.Precision != 24 || f.Scale != 15) {
				t.Fatalf("source field changed: %#v", f)
			}
		}
	}
}

func TestPreviewDecimalJSONStillRejectsFloatProviderValues(t *testing.T) {
	for _, value := range []interface{}{float64(1.25), float32(1.25)} {
		table := &models.TablePreview{Mode: preview.PreviewModeTable, Fields: []datatype.FieldInfo{{Name: "amount", Type: datatype.FieldTypeDecimal}}, Rows: []map[string]interface{}{{"amount": value}}}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/preview", nil)
		writePreviewResult(c, &preview.PreviewResult{Data: table}, nil, dataprotection.SubjectReference{}, nil)
		if w.Code != 500 {
			t.Fatalf("inexact decimal silently accepted: %d %s", w.Code, w.Body.String())
		}
	}
}

type shapefilePreviewSource struct{ base string }

func (r shapefilePreviewSource) Open(_ context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	return os.Open(r.base + filepath.Ext(ref.Path))
}
func (r shapefilePreviewSource) Stat(_ context.Context, ref contentio.Ref) (*contentio.Stat, error) {
	s, e := os.Stat(r.base + filepath.Ext(ref.Path))
	if e != nil {
		return nil, e
	}
	return &contentio.Stat{Ref: ref, Exists: true, Size: s.Size()}, nil
}
