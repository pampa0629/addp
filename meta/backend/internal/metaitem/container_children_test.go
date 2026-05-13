package metaitem

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
	"github.com/addp/meta/internal/dataitem"
	"github.com/addp/meta/internal/models"
	_ "github.com/mattn/go-sqlite3"
	"github.com/xuri/excelize/v2"
)

func TestEnrichExcelContainerChildrenWritesSheets(t *testing.T) {
	t.Parallel()

	workbook := excelize.NewFile()
	defer workbook.Close()
	index, err := workbook.NewSheet("Cities")
	if err != nil {
		t.Fatalf("new sheet: %v", err)
	}
	workbook.SetActiveSheet(index)
	if err := workbook.SetSheetRow("Cities", "A1", &[]interface{}{"id", "name"}); err != nil {
		t.Fatalf("set header: %v", err)
	}
	if err := workbook.SetSheetRow("Cities", "A2", &[]interface{}{1, "Hangzhou"}); err != nil {
		t.Fatalf("set row: %v", err)
	}
	var buf bytes.Buffer
	if err := workbook.Write(&buf); err != nil {
		t.Fatalf("write workbook: %v", err)
	}

	attrs := models.JSONMap{}
	item := &DetectedItem{DataType: dataitem.DataTypeContainer, Format: string(format.FormatExcel)}
	if err := EnrichContainerChildren(context.Background(), attrs, item, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("enrich: %v", err)
	}

	container := attrs["type_info"].(map[string]interface{})["container"].(map[string]interface{})
	if container["child_count"] != 2 {
		t.Fatalf("child_count = %v, want 2", container["child_count"])
	}
	children := container["children"].([]map[string]interface{})
	if len(children) != 2 || children[0]["kind"] != "sheet" {
		t.Fatalf("children = %#v", children)
	}
}

func TestEnrichSQLiteContainerChildrenWritesTables(t *testing.T) {
	t.Parallel()

	data := sqliteDatabaseBytes(t, func(db *sql.DB) {
		if _, err := db.Exec(`CREATE TABLE people (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
			t.Fatalf("create table: %v", err)
		}
	})

	attrs := models.JSONMap{}
	item := &DetectedItem{DataType: dataitem.DataTypeContainer, Format: string(format.FormatSQLite)}
	if err := EnrichContainerChildren(context.Background(), attrs, item, bytes.NewReader(data)); err != nil {
		t.Fatalf("enrich: %v", err)
	}

	container := attrs["type_info"].(map[string]interface{})["container"].(map[string]interface{})
	children := container["children"].([]map[string]interface{})
	if len(children) != 1 || children[0]["name"] != "people" {
		t.Fatalf("children = %#v", children)
	}
	if _, ok := children[0]["fields"]; ok {
		t.Fatalf("container child should not carry fields: %#v", children[0])
	}
	if children[0]["column_count"] != 2 {
		t.Fatalf("column_count = %#v, want 2", children[0]["column_count"])
	}
}

func TestEnrichGeoPackageContainerChildrenWritesLightweightLayers(t *testing.T) {
	t.Parallel()

	data := sqliteDatabaseBytes(t, func(db *sql.DB) {
		stmts := []string{
			`CREATE TABLE gpkg_contents (table_name TEXT PRIMARY KEY, data_type TEXT NOT NULL, identifier TEXT, srs_id INTEGER, min_x DOUBLE, min_y DOUBLE, max_x DOUBLE, max_y DOUBLE)`,
			`CREATE TABLE gpkg_geometry_columns (table_name TEXT, column_name TEXT, geometry_type_name TEXT, srs_id INTEGER)`,
			`CREATE TABLE roads (id INTEGER PRIMARY KEY, geom BLOB, name TEXT)`,
			`CREATE VIRTUAL TABLE rtree_roads_geom USING rtree(id, minx, maxx, miny, maxy)`,
			`INSERT INTO gpkg_contents(table_name, data_type, identifier, srs_id, min_x, min_y, max_x, max_y) VALUES ('roads', 'features', 'Road Layer', 4326, 120.0, 30.0, 121.0, 31.0)`,
			`INSERT INTO gpkg_geometry_columns(table_name, column_name, geometry_type_name, srs_id) VALUES ('roads', 'geom', 'LINESTRING', 4326)`,
		}
		for _, stmt := range stmts {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("exec %q: %v", stmt, err)
			}
		}
	})

	attrs := models.JSONMap{}
	item := &DetectedItem{DataType: dataitem.DataTypeContainer, Format: string(format.FormatGeoPackage)}
	if err := EnrichContainerChildren(context.Background(), attrs, item, bytes.NewReader(data)); err != nil {
		t.Fatalf("enrich: %v", err)
	}

	container := attrs["type_info"].(map[string]interface{})["container"].(map[string]interface{})
	children := container["children"].([]map[string]interface{})
	if len(children) != 1 || children[0]["kind"] != "layer" || children[0]["name"] != "Road Layer" {
		t.Fatalf("children = %#v", children)
	}
	if container["child_count"] != 1 {
		t.Fatalf("child_count = %#v, want visible layer count 1", container["child_count"])
	}
	if children[0]["table"] != "roads" {
		t.Fatalf("child table = %#v, want roads", children[0]["table"])
	}
	if _, ok := children[0]["fields"]; ok {
		t.Fatalf("container child should not carry fields: %#v", children[0])
	}
	if _, ok := children[0]["rows"]; ok {
		t.Fatalf("container child should not carry rows: %#v", children[0])
	}
	if _, ok := attrs["capabilities"]; ok {
		t.Fatalf("container should not carry child spatial capability: %#v", attrs["capabilities"])
	}
	if children[0]["column_count"] != 3 {
		t.Fatalf("column_count = %#v, want 3", children[0]["column_count"])
	}
}

func TestEnrichZIPContainerChildrenWritesLightweightEntries(t *testing.T) {
	t.Parallel()

	data := zipContainerBytes(t, map[string]string{
		"data/cities.csv":  "id,name\n1,Hangzhou\n",
		"notes/readme.txt": "hello",
	})

	attrs := models.JSONMap{}
	item := &DetectedItem{DataType: dataitem.DataTypeContainer, Format: string(format.FormatZIP)}
	if err := EnrichContainerChildren(context.Background(), attrs, item, bytes.NewReader(data)); err != nil {
		t.Fatalf("enrich: %v", err)
	}

	container := attrs["type_info"].(map[string]interface{})["container"].(map[string]interface{})
	children := container["children"].([]map[string]interface{})
	if len(children) != 2 || children[0]["name"] != "data/cities.csv" {
		t.Fatalf("children = %#v", children)
	}
	if children[0]["format"] != string(format.FormatCSV) {
		t.Fatalf("child format = %#v, want csv", children[0]["format"])
	}
	if _, ok := children[0]["fields"]; ok {
		t.Fatalf("zip child should not carry fields: %#v", children[0])
	}
}

func sqliteDatabaseBytes(t *testing.T, setup func(*sql.DB)) []byte {
	t.Helper()

	tmp, err := os.CreateTemp("", "metaitem-container-*.db")
	if err != nil {
		t.Fatalf("temp db: %v", err)
	}
	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		t.Fatalf("close temp: %v", err)
	}
	defer os.Remove(path)

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	setup(db)
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sqlite: %v", err)
	}
	return data
}

func zipContainerBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, body := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}
