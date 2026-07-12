package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	_ "github.com/mattn/go-sqlite3"
)

func TestDescribeTableRequiresExplicitTableOption(t *testing.T) {
	t.Parallel()

	plugin := NewPlugin(nil)
	_, err := plugin.DescribeTable(context.Background(), bytes.NewReader(sqliteTestDatabaseBytes(t)), nil)
	if err == nil {
		t.Fatal("DescribeTable() error = nil, want explicit table option error")
	}
}

func TestDescribeTableUsesSelectedTable(t *testing.T) {
	t.Parallel()

	plugin := NewPlugin(nil)
	info, err := plugin.DescribeTable(context.Background(), bytes.NewReader(sqliteTestDatabaseBytes(t)), &format.ParseOptions{
		ExtraParams: map[string]interface{}{format.ChildTableParam: "cities"},
	})
	if err != nil {
		t.Fatalf("DescribeTable() error = %v", err)
	}
	if info.Table.Name != "cities" {
		t.Fatalf("Name = %q, want cities", info.Table.Name)
	}
	if info.Table.Kind != "table" {
		t.Fatalf("Kind = %q, want table", info.Table.Kind)
	}
	if len(info.Table.Native) != 0 {
		t.Fatalf("Native = %#v, want empty for SQLite table kind", info.Table.Native)
	}
	if len(info.Table.Fields) != 2 || info.Table.Fields[0].Name != "id" || info.Table.Fields[1].Name != "name" {
		t.Fatalf("Fields = %#v, want id/name", info.Table.Fields)
	}
}

func TestDescribeTableMarksSQLiteViewKind(t *testing.T) {
	t.Parallel()

	plugin := NewPlugin(nil)
	info, err := plugin.DescribeTable(context.Background(), bytes.NewReader(sqliteViewTestDatabaseBytes(t)), &format.ParseOptions{
		ExtraParams: map[string]interface{}{format.ChildTableParam: "city_names"},
	})
	if err != nil {
		t.Fatalf("DescribeTable() error = %v", err)
	}
	if info.Table.Kind != "view" {
		t.Fatalf("Kind = %q, want view", info.Table.Kind)
	}
}

func TestDescribeContainerReturnsLightweightChildren(t *testing.T) {
	t.Parallel()

	plugin := NewPlugin(nil)
	info, err := plugin.DescribeContainer(context.Background(), bytes.NewReader(sqliteTestDatabaseBytes(t)), &format.ParseOptions{
		ExtraParams: map[string]interface{}{
			format.ContainerChildLimitParam: 0,
			format.ContainerRowLimitParam:   0,
		},
	})
	if err != nil {
		t.Fatalf("DescribeContainer() error = %v", err)
	}
	if info.ChildCount != 2 || len(info.Children) != 2 {
		t.Fatalf("container children = %#v, want 2", info.Children)
	}
	if info.Children[0].ColumnCount == nil {
		t.Fatalf("container child column_count missing: %#v", info.Children[0])
	}
}

func TestDescribeGeoPackageContainerReturnsLightweightLayers(t *testing.T) {
	t.Parallel()

	plugin := NewGeoPackagePlugin(nil)
	info, err := plugin.DescribeContainer(context.Background(), bytes.NewReader(geoPackageTestDatabaseBytes(t)), &format.ParseOptions{
		ExtraParams: map[string]interface{}{
			format.ContainerChildLimitParam: 0,
			format.ContainerRowLimitParam:   0,
		},
	})
	if err != nil {
		t.Fatalf("DescribeContainer() error = %v", err)
	}
	if len(info.Children) != 1 {
		t.Fatalf("children = %#v, want one layer", info.Children)
	}
	if info.ChildCount != 1 {
		t.Fatalf("ChildCount = %d, want visible layer count 1", info.ChildCount)
	}
	child := info.Children[0]
	if child.Name != "Road Layer" || child.ChildKind != "layer" || child.DataType != datatype.Table {
		t.Fatalf("child = %#v, want Road Layer layer", child)
	}
	if child.Native["table"] != "roads" {
		t.Fatalf("child table = %#v, want roads", child.Native["table"])
	}
	if child.ColumnCount == nil || *child.ColumnCount != 3 {
		t.Fatalf("column_count = %#v, want 3", child.ColumnCount)
	}
}

func TestUDBXDescriptorUsesSuperMapExtension(t *testing.T) {
	t.Parallel()

	descriptor := NewUDBXPlugin(nil).Descriptor()
	if descriptor.Format != format.FormatUDBX {
		t.Fatalf("Format = %q, want udbx", descriptor.Format)
	}
	if descriptor.DataType != datatype.Container {
		t.Fatalf("DataType = %q, want container", descriptor.DataType)
	}
	if len(descriptor.Identification.Extensions) != 1 || descriptor.Identification.Extensions[0] != ".udbx" {
		t.Fatalf("Extensions = %#v, want .udbx", descriptor.Identification.Extensions)
	}
}

func TestDescribeGeoPackageTableCarriesChildSpatialInfo(t *testing.T) {
	t.Parallel()

	plugin := NewGeoPackagePlugin(nil)
	info, err := plugin.DescribeTable(context.Background(), bytes.NewReader(geoPackageTestDatabaseBytes(t)), &format.ParseOptions{
		ExtraParams: map[string]interface{}{format.ChildTableParam: "roads"},
	})
	if err != nil {
		t.Fatalf("DescribeTable() error = %v", err)
	}
	if field := info.Table.GetField("geom"); field == nil || field.Type != datatype.FieldTypeGeometry {
		t.Fatalf("geom field = %#v, want geometry", field)
	}
	spatial := info.Spatial
	if spatial == nil {
		t.Fatal("spatial info missing")
	}
	if spatial.PrimaryGeometryName() != "geom" || spatial.PrimaryGeometryType() != "LineString" || spatial.PrimarySRIDValue() != 4326 {
		t.Fatalf("spatial = %#v", spatial)
	}
	if spatial.HasSpatialIndex == nil || !*spatial.HasSpatialIndex {
		t.Fatalf("spatial index = false, want true")
	}
	if spatial.Extent == nil || *spatial.Extent != (datatype.BoundingBox{120.0, 30.0, 121.0, 31.0}) {
		t.Fatalf("bbox = %#v", spatial.Extent)
	}
}

func TestAnalyzeTableLimitZeroListsAllTables(t *testing.T) {
	t.Parallel()

	db, cleanup := openSQLiteTestDatabase(t, sqliteTestDatabaseBytes(t))
	defer cleanup()

	opts := DefaultOptions()
	opts.TableLimit = 0
	opts.SampleRowLimit = 0
	result, err := Analyze(context.Background(), db, &opts)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(result.Metadata.Tables) != 2 {
		t.Fatalf("tables = %#v, want 2 tables", result.Metadata.Tables)
	}
}

func sqliteTestDatabaseBytes(t *testing.T) []byte {
	t.Helper()

	tmp, err := os.CreateTemp("", "sqlite-plugin-test-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		t.Fatalf("close temp db: %v", err)
	}
	defer os.Remove(path)

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE animals (name TEXT)`,
		`CREATE TABLE cities (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sqlite: %v", err)
	}
	return data
}

func sqliteViewTestDatabaseBytes(t *testing.T) []byte {
	t.Helper()

	tmp, err := os.CreateTemp("", "sqlite-plugin-view-test-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		t.Fatalf("close temp db: %v", err)
	}
	defer os.Remove(path)

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE cities (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`,
		`CREATE VIEW city_names AS SELECT name FROM cities`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sqlite: %v", err)
	}
	return data
}

func geoPackageTestDatabaseBytes(t *testing.T) []byte {
	t.Helper()

	tmp, err := os.CreateTemp("", "gpkg-plugin-test-*.gpkg")
	if err != nil {
		t.Fatalf("create temp gpkg: %v", err)
	}
	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		t.Fatalf("close temp gpkg: %v", err)
	}
	defer os.Remove(path)

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open gpkg: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE gpkg_contents (table_name TEXT PRIMARY KEY, data_type TEXT NOT NULL, identifier TEXT, srs_id INTEGER, min_x DOUBLE, min_y DOUBLE, max_x DOUBLE, max_y DOUBLE)`,
		`CREATE TABLE gpkg_geometry_columns (table_name TEXT, column_name TEXT, geometry_type_name TEXT, srs_id INTEGER)`,
		`CREATE TABLE roads (id INTEGER PRIMARY KEY, geom BLOB, name TEXT)`,
		`CREATE VIRTUAL TABLE rtree_roads_geom USING rtree(id, minx, maxx, miny, maxy)`,
		`INSERT INTO gpkg_contents(table_name, data_type, identifier, srs_id, min_x, min_y, max_x, max_y) VALUES ('roads', 'features', 'Road Layer', 4326, 120.0, 30.0, 121.0, 31.0)`,
		`INSERT INTO gpkg_geometry_columns(table_name, column_name, geometry_type_name, srs_id) VALUES ('roads', 'geom', 'LINESTRING', 4326)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close gpkg: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read gpkg: %v", err)
	}
	return data
}

func openSQLiteTestDatabase(t *testing.T, data []byte) (*sql.DB, func()) {
	t.Helper()

	tmp, err := os.CreateTemp("", "sqlite-plugin-open-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	path := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		t.Fatalf("write temp db: %v", err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatalf("close temp db: %v", err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		os.Remove(path)
		t.Fatalf("open sqlite: %v", err)
	}
	return db, func() {
		_ = db.Close()
		_ = os.Remove(path)
	}
}
