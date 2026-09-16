package metaenrich

import (
	"context"
	"database/sql"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/xuri/excelize/v2"
)

func TestResourceEnrichmentKeepsContainerIdentity(t *testing.T) {
	sqliteData := sqliteDatabaseBytes(t, func(db *sql.DB) {
		if _, err := db.Exec(`CREATE TABLE people (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
			t.Fatal(err)
		}
	})
	workbook := excelize.NewFile()
	defer workbook.Close()
	if err := workbook.SetSheetRow("Sheet1", "A1", &[]interface{}{"id", "name"}); err != nil {
		t.Fatal(err)
	}
	excelData, err := workbook.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		format format.FormatType
		path   string
		data   []byte
	}{
		{format.FormatSQLite, "sample.sqlite", sqliteData},
		{format.FormatGeoPackage, "sample.gpkg", sqliteData},
		{format.FormatUDBX, "sample.udbx", sqliteData},
		{format.FormatExcel, "sample.xlsx", excelData.Bytes()},
	} {
		for _, initialType := range []datatype.DataType{datatype.Container, datatype.Table, datatype.Unknown} {
			t.Run(tc.path+"/"+string(initialType), func(t *testing.T) {
				item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{
					Layout: format.LayoutSingle, DataType: initialType, Format: string(tc.format), PrimaryContentPath: tc.path,
				}}
				if initialType == datatype.Table {
					item.Fields = []datatype.FieldInfo{{Name: "stale", Type: datatype.FieldTypeInt}}
					item.Attributes = map[string]interface{}{
						"type_info":    map[string]interface{}{"table": map[string]interface{}{"row_count": 99}},
						"access_index": map[string]interface{}{"table": map[string]interface{}{"kind": "stale"}},
						"capabilities": map[string]interface{}{"spatial": map[string]interface{}{"srid": 4326}},
					}
				}
				attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))
				enriched, fields, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
					Item: item, ContentReader: staticContentReader{content: string(tc.data)}, EngineID: 1,
					PhysicalPath: tc.path, SizeBytes: int64(len(tc.data)),
					EngineCatalogPathFor: func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(1, path) },
				})
				if err != nil {
					t.Fatal(err)
				}
				if enriched.DataType != datatype.Container || len(fields) != 0 {
					t.Fatalf("parent type=%s fields=%v, want container without fields", enriched.DataType, fields)
				}
				if got := commonJSON.Section(attrs, "item")["data_type"]; got != string(datatype.Container) {
					t.Fatalf("persisted type=%v", got)
				}
				container := commonJSON.Section(attrs, "type_info.container")
				if commonJSON.InterfaceInt64(container["child_count"]) != 1 {
					t.Fatalf("container=%#v", container)
				}
				for _, path := range []string{"type_info.table", "access_index.table", "capabilities.spatial"} {
					if len(commonJSON.Section(attrs, path)) != 0 {
						t.Fatalf("stale parent facts remain at %s", path)
					}
				}
			})
		}
	}
}

func TestResourceEnrichmentReportsMalformedContainer(t *testing.T) {
	item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{Layout: format.LayoutSingle, DataType: datatype.Container, Format: string(format.FormatZIP)}}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))
	_, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		Item: item, ContentReader: staticContentReader{content: "invalid zip"}, PhysicalPath: "broken.zip",
		EngineCatalogPathFor: func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(1, path) },
	})
	if err == nil {
		t.Fatal("malformed container must not be treated as successful empty container")
	}
}
