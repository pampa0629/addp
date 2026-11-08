package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func standaloneVectorBytes(t *testing.T, changes ...string) []byte {
	t.Helper()
	name := filepath.Join(t.TempDir(), "standard.gpkg")
	db, err := sql.Open("sqlite3", name)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range append([]string{
		`PRAGMA application_id=1196444487`, `PRAGMA user_version=10300`,
		`CREATE TABLE gpkg_spatial_ref_sys(srs_name TEXT NOT NULL,srs_id INTEGER PRIMARY KEY,organization TEXT NOT NULL,organization_coordsys_id INTEGER NOT NULL,definition TEXT NOT NULL,description TEXT)`,
		`INSERT INTO gpkg_spatial_ref_sys VALUES('WGS84',4326,'EPSG',4326,'GEOGCS["WGS84"]',NULL)`,
		`CREATE TABLE gpkg_contents(table_name TEXT PRIMARY KEY,data_type TEXT NOT NULL,identifier TEXT,last_change TEXT,srs_id INTEGER, min_x DOUBLE,min_y DOUBLE,max_x DOUBLE,max_y DOUBLE)`,
		`CREATE TABLE gpkg_geometry_columns(table_name TEXT PRIMARY KEY,column_name TEXT NOT NULL,geometry_type_name TEXT NOT NULL,srs_id INTEGER NOT NULL,z INTEGER NOT NULL,m INTEGER NOT NULL)`,
		`CREATE TABLE features(fid INTEGER PRIMARY KEY, actual_shape BLOB, geometry TEXT)`,
		`INSERT INTO gpkg_contents VALUES('features','features','Features','2026-11-08',4326,NULL,NULL,NULL,NULL)`,
		`INSERT INTO gpkg_geometry_columns VALUES('features','actual_shape','GEOMETRY',4326,0,0)`,
	}, changes...) {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			t.Fatal(stmt, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestGeoPackageStandaloneVectorObservation(t *testing.T) {
	for name, changes := range map[string][]string{
		"core": nil,
		"rtree": {
			`CREATE TABLE gpkg_extensions(table_name TEXT,column_name TEXT,extension_name TEXT,definition TEXT,scope TEXT)`,
			`CREATE VIRTUAL TABLE rtree_features_actual_shape USING rtree(id,minx,maxx,miny,maxy)`,
			`INSERT INTO gpkg_extensions VALUES('features','actual_shape','gpkg_rtree_index','http://www.geopackage.org/spec/#extension_rtree','write-only')`,
		},
		"crs wkt": {
			`ALTER TABLE gpkg_spatial_ref_sys ADD COLUMN definition_12_063 TEXT`,
			`CREATE TABLE gpkg_extensions(table_name TEXT,column_name TEXT,extension_name TEXT,definition TEXT,scope TEXT)`,
			`INSERT INTO gpkg_extensions VALUES('gpkg_spatial_ref_sys','definition_12_063','gpkg_crs_wkt','http://www.geopackage.org/spec/#extension_crs_wkt','read-write')`,
		},
		"multiple layers": {
			`CREATE TABLE second(fid INTEGER PRIMARY KEY, shape BLOB)`,
			`INSERT INTO gpkg_contents VALUES('second','features','Second','2026-11-08',4326,NULL,NULL,NULL,NULL)`,
			`INSERT INTO gpkg_geometry_columns VALUES('second','shape','MULTIPOLYGON',4326,0,0)`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			raw := standaloneVectorBytes(t, changes...)
			original := append([]byte(nil), raw...)
			info, err := NewGeoPackagePlugin(nil).DescribeFormat(t.Context(), bytes.NewReader(raw), nil)
			if err != nil || info["standalone_vector"] != true {
				t.Fatal("standard standalone vector not observed", info, err)
			}
			if _, grantsComposition := info["composition_complete"]; grantsComposition || !bytes.Equal(raw, original) {
				t.Fatal("byte-only observation granted source completeness or modified input")
			}
		})
	}
}

func TestGeoPackageStandaloneRejectsUnsupportedAndBrokenContainers(t *testing.T) {
	for name, changes := range map[string][]string{
		"plain SQLite":      {`PRAGMA application_id=0`},
		"future version":    {`PRAGMA user_version=20000`},
		"old profile":       {`PRAGMA user_version=10100`},
		"tiles":             {`UPDATE gpkg_contents SET data_type='tiles'`},
		"unbound column":    {`UPDATE gpkg_geometry_columns SET column_name='absent'`},
		"unbound SRS":       {`UPDATE gpkg_geometry_columns SET srs_id=9999`},
		"missing SRS":       {`DELETE FROM gpkg_spatial_ref_sys`},
		"unsupported shape": {`UPDATE gpkg_geometry_columns SET geometry_type_name='CIRCULARSTRING'`},
		"view":              {`CREATE VIEW external_values AS SELECT * FROM features`},
		"generated":         {`CREATE TABLE generated(a INTEGER,b INTEGER GENERATED ALWAYS AS (a+1))`},
		"undeclared index":  {`CREATE VIRTUAL TABLE rtree_features_actual_shape USING rtree(id,minx,maxx,miny,maxy)`},
		"external extension": {
			`CREATE TABLE gpkg_extensions(table_name TEXT,column_name TEXT,extension_name TEXT,definition TEXT,scope TEXT)`,
			`INSERT INTO gpkg_extensions VALUES('features','actual_shape','vendor_external','https://example.invalid','read-write')`,
		},
		"unbound crs extension": {
			`CREATE TABLE gpkg_extensions(table_name TEXT,column_name TEXT,extension_name TEXT,definition TEXT,scope TEXT)`,
			`INSERT INTO gpkg_extensions VALUES('gpkg_spatial_ref_sys','definition_12_063','gpkg_crs_wkt','http://www.geopackage.org/spec/#extension_crs_wkt','read-write')`,
		},
		"mismatched index": {
			`CREATE TABLE gpkg_extensions(table_name TEXT,column_name TEXT,extension_name TEXT,definition TEXT,scope TEXT)`,
			`CREATE VIRTUAL TABLE rtree_features_actual_shape USING rtree(id,minx,maxx,miny,maxy)`,
			`INSERT INTO gpkg_extensions VALUES('features','wrong_column','gpkg_rtree_index','http://www.geopackage.org/spec/#extension_rtree','write-only')`,
		},
		"unbound empty index": {
			`CREATE TABLE gpkg_extensions(table_name TEXT,column_name TEXT,extension_name TEXT,definition TEXT,scope TEXT)`,
			`CREATE VIRTUAL TABLE rtree__ USING rtree(id,minx,maxx,miny,maxy)`,
			`INSERT INTO gpkg_extensions VALUES('','','gpkg_rtree_index','http://www.geopackage.org/spec/#extension_rtree','write-only')`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			info, err := NewGeoPackagePlugin(nil).DescribeFormat(t.Context(), bytes.NewReader(standaloneVectorBytes(t, changes...)), nil)
			if err == nil && info["standalone_vector"] != false {
				t.Fatal("unsupported file admitted", info)
			}
		})
	}
	for _, change := range []string{"WAL", "truncated", "trailing bytes", "wrong count", "invalid pages"} {
		t.Run(change, func(t *testing.T) {
			raw := standaloneVectorBytes(t)
			switch change {
			case "WAL":
				raw[18], raw[19] = 2, 2
			case "truncated":
				raw = raw[:len(raw)-1]
			case "trailing bytes":
				raw = append(raw, 0)
			case "wrong count":
				binary.BigEndian.PutUint32(raw[28:32], 1)
			case "invalid pages":
				raw[100] = 255
			}
			info, err := NewGeoPackagePlugin(nil).DescribeFormat(t.Context(), bytes.NewReader(raw), nil)
			if err == nil && info["standalone_vector"] == true {
				t.Fatal("broken file admitted", info)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := NewGeoPackagePlugin(nil).DescribeFormat(ctx, bytes.NewReader(standaloneVectorBytes(t)), nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}
