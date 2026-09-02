package executor

import (
	"context"
	"io"
	"testing"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/resume"
)

func TestProtectedTableBatchReaderProtectsRowsAndSchemaBeforeConsumption(t *testing.T) {
	inner := &protectionTestBatchReader{
		info: &datatype.TableInfo{
			Name: "persons",
			Fields: []datatype.FieldInfo{
				{Name: "id", Path: []string{"id"}, Type: datatype.FieldTypeInt},
				{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString},
				{Name: "secret", Path: []string{"secret"}, Type: datatype.FieldTypeString},
			},
			PrimaryKey: []string{"id", "secret"},
		},
		batch: &engineplugin.BatchData{
			Fields: []datatype.FieldInfo{
				{Name: "id", Path: []string{"id"}, Type: datatype.FieldTypeInt},
				{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString},
				{Name: "secret", Path: []string{"secret"}, Type: datatype.FieldTypeString},
			},
			Rows: []map[string]interface{}{{"id": 1, "phone": "13661384499", "secret": "raw"}},
		},
	}
	protect := func(result *engineplugin.QueryResult) error {
		result.Columns = []string{"id", "phone"}
		for _, row := range result.Rows {
			row["phone"] = "136****4499"
			delete(row, "secret")
		}
		return nil
	}

	reader, err := protectTableBatchReader(inner, protect)
	if err != nil {
		t.Fatal(err)
	}
	if got := reader.TableInfo().FieldNames(); len(got) != 2 || got[0] != "id" || got[1] != "phone" {
		t.Fatalf("protected table fields = %#v", got)
	}
	if got := reader.TableInfo().PrimaryKey; len(got) != 1 || got[0] != "id" {
		t.Fatalf("protected primary key = %#v", got)
	}
	batch, err := reader.ReadBatch(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := batch.Rows[0]["phone"]; got != "136****4499" {
		t.Fatalf("protected phone = %#v", got)
	}
	if _, exists := batch.Rows[0]["secret"]; exists {
		t.Fatal("suppressed field remained in protected row")
	}
	if got := batchFieldNames(batch.Fields); len(got) != 2 || got[0] != "id" || got[1] != "phone" {
		t.Fatalf("protected batch fields = %#v", got)
	}
}

func TestProtectedTableBatchReaderLeavesSpatialMetadataWhenSchemaIsDeferred(t *testing.T) {
	inner := &protectionTestBatchReader{
		spatial: datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 2),
		batch:   &engineplugin.BatchData{Rows: []map[string]interface{}{{"geom": "POINT (1 2)"}}},
	}
	reader, err := protectTableBatchReader(inner, func(*engineplugin.QueryResult) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if reader.SpatialInfo() == nil || reader.SpatialInfo().PrimaryGeometryColumn != "geom" {
		t.Fatalf("deferred spatial info = %#v", reader.SpatialInfo())
	}
}

type protectionTestBatchReader struct {
	info    *datatype.TableInfo
	spatial *datatype.SpatialInfo
	batch   *engineplugin.BatchData
	read    bool
}

func (r *protectionTestBatchReader) TableInfo() *datatype.TableInfo     { return r.info }
func (r *protectionTestBatchReader) SpatialInfo() *datatype.SpatialInfo { return r.spatial }
func (r *protectionTestBatchReader) Close(context.Context) error        { return nil }
func (r *protectionTestBatchReader) ResumeMarker() *resume.Marker       { return nil }
func (r *protectionTestBatchReader) ReadBatch(context.Context, int) (*engineplugin.BatchData, error) {
	if r.read {
		return nil, io.EOF
	}
	r.read = true
	return r.batch, nil
}
