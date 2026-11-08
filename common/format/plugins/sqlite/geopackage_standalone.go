package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"

	"github.com/addp/common/format"
)

// describeGeoPackageFormat observes the same complete bytes as the existing
// deep reader. It never opens the original business path or replays a journal.
// Source stability and absence of sidecars must separately be proved by Meta.
func (p *Plugin) describeGeoPackageFormat(ctx context.Context, input io.Reader, options *format.ParseOptions) (map[string]interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name, cleanup, err := p.saveToTempFile(input)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	var header [100]byte
	_, readErr := io.ReadFull(f, header[:])
	stat, statErr := f.Stat()
	closeErr := f.Close()
	if readErr != nil || statErr != nil || closeErr != nil {
		return nil, fmt.Errorf("read GeoPackage container header: %w", errors.Join(readErr, statErr, closeErr))
	}
	if !bytes.Equal(header[:16], []byte("SQLite format 3\x00")) {
		return nil, fmt.Errorf("invalid GeoPackage SQLite header")
	}
	uri := (&url.URL{Scheme: "file", Path: name}).String() + "?mode=ro&immutable=1&_query_only=1"
	db, err := sql.Open("sqlite3", uri)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "PRAGMA trusted_schema=OFF"); err != nil {
		return nil, err
	}
	complete, err := geoPackageStandalone(ctx, db, header[:], stat.Size())
	if err != nil {
		return nil, err
	}
	// Existing metadata discovery is preserved for unsupported profiles. This
	// flag alone is deliberately not the DataItem composition_complete fact.
	result, err := Analyze(ctx, db, p.analysisOptions(options))
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"version": result.Metadata.Version, "page_size": result.Metadata.PageSize,
		"page_count": result.Metadata.PageCount, "table_count": result.Metadata.TableCount,
		"view_count": result.Metadata.ViewCount, "index_count": result.Metadata.IndexCount,
		"standalone_vector": complete,
	}, nil
}

var standardGeoPackageRTree = regexp.MustCompile(`(?i)\bUSING\s+rtree\s*\(\s*id\s*,\s*minx\s*,\s*maxx\s*,\s*miny\s*,\s*maxy\s*\)\s*$`)

func geoPackageStandalone(ctx context.Context, db *sql.DB, header []byte, size int64) (bool, error) {
	version := binary.BigEndian.Uint32(header[60:64])
	pageSize := int64(binary.BigEndian.Uint16(header[16:18]))
	if pageSize == 1 {
		pageSize = 65536
	}
	if binary.BigEndian.Uint32(header[68:72]) != 0x47504b47 || version < 10200 || version > 10499 ||
		header[18] != 1 || header[19] != 1 || pageSize < 512 || pageSize > 65536 || pageSize&(pageSize-1) != 0 ||
		size%pageSize != 0 || int64(binary.BigEndian.Uint32(header[28:32]))*pageSize != size ||
		!bytes.Equal(header[24:28], header[92:96]) {
		return false, nil
	}
	// Inspect schema before querying its declared tables; no view, generated
	// expression or unknown virtual module can supply the proof.
	rows, err := db.QueryContext(ctx, `SELECT s.name, p.type, COALESCE(s.sql,'') FROM sqlite_master s
JOIN pragma_table_list p ON p.name=s.name AND p.schema='main' WHERE s.type IN ('table','view')`)
	if err != nil {
		return false, err
	}
	tables := map[string]bool{}
	virtual := map[string]string{}
	unsupported := false
	for rows.Next() {
		var name, kind, ddl string
		if err := rows.Scan(&name, &kind, &ddl); err != nil {
			rows.Close()
			return false, err
		}
		if kind == "view" {
			unsupported = true
		}
		tables[name] = kind == "table" || kind == "virtual" || kind == "shadow"
		if kind == "virtual" {
			virtual[name] = ddl
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || unsupported {
		return false, err
	}
	for _, name := range []string{"gpkg_contents", "gpkg_geometry_columns", "gpkg_spatial_ref_sys"} {
		if !tables[name] || virtual[name] != "" {
			return false, nil
		}
	}
	for name := range tables {
		if virtual[name] != "" {
			continue
		}
		var hidden int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_xinfo(?) WHERE hidden != 0`, name).Scan(&hidden); err != nil {
			return false, err
		}
		if hidden != 0 {
			return false, nil
		}
	}
	rows, err = db.QueryContext(ctx, `SELECT c.table_name, c.data_type, c.srs_id, g.column_name, g.geometry_type_name, g.srs_id, g.z, g.m
FROM gpkg_contents c LEFT JOIN gpkg_geometry_columns g ON g.table_name=c.table_name`)
	if err != nil {
		return false, ctx.Err() // An incompatible core schema is not admitted.
	}
	geometry := map[string]string{}
	for rows.Next() {
		var table, kind, column, shape string
		var srid, geometrySRID, z, m int64
		if err := rows.Scan(&table, &kind, &srid, &column, &shape, &geometrySRID, &z, &m); err != nil {
			unsupported = true
			break
		}
		validShape := false
		switch shape {
		case "GEOMETRY", "POINT", "LINESTRING", "POLYGON", "MULTIPOINT", "MULTILINESTRING", "MULTIPOLYGON", "GEOMETRYCOLLECTION":
			validShape = true
		}
		if kind != "features" || table == "" || !tables[table] || virtual[table] != "" || geometry[table] != "" || column == "" ||
			!validShape || srid != geometrySRID || z < 0 || z > 2 || m < 0 || m > 2 {
			unsupported = true
			break
		}
		geometry[table] = column
	}
	err = rows.Err()
	rows.Close()
	if err != nil || unsupported || len(geometry) == 0 {
		return false, err
	}
	var bad int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM gpkg_geometry_columns g
LEFT JOIN gpkg_contents c ON c.table_name=g.table_name
LEFT JOIN gpkg_spatial_ref_sys s ON s.srs_id=g.srs_id
WHERE c.table_name IS NULL OR s.srs_id IS NULL`).Scan(&bad); err != nil || bad != 0 {
		return false, err
	}
	for table, column := range geometry {
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_xinfo(?) WHERE name=?`, table, column).Scan(&bad); err != nil || bad != 1 {
			return false, err
		}
	}
	indexes := map[string]bool{}
	crsWKT := false
	if tables["gpkg_extensions"] {
		if virtual["gpkg_extensions"] != "" {
			return false, nil
		}
		rows, err = db.QueryContext(ctx, `SELECT table_name,column_name,extension_name,scope FROM gpkg_extensions`)
		if err != nil {
			return false, ctx.Err()
		}
		for rows.Next() {
			var table, column, name, scope string
			if err := rows.Scan(&table, &column, &name, &scope); err != nil {
				unsupported = true
				break
			}
			switch name {
			case "gpkg_rtree_index":
				index := "rtree_" + table + "_" + column
				if geometry[table] == "" || geometry[table] != column || scope != "write-only" || !standardGeoPackageRTree.MatchString(virtual[index]) || indexes[index] {
					unsupported = true
				}
				indexes[index] = true
			case "gpkg_crs_wkt":
				if table != "gpkg_spatial_ref_sys" || column != "definition_12_063" || scope != "read-write" || crsWKT {
					unsupported = true
				}
				crsWKT = true
			default:
				unsupported = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil || unsupported {
			return false, err
		}
	}
	for name := range virtual {
		if !indexes[name] {
			return false, nil
		}
	}
	if crsWKT {
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_xinfo('gpkg_spatial_ref_sys') WHERE name='definition_12_063'`).Scan(&bad); err != nil || bad != 1 {
			return false, err
		}
	}
	var integrity string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&integrity); err != nil {
		return false, err
	}
	if integrity != "ok" {
		return false, nil
	}
	rows, err = db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return false, err
	}
	violations := rows.Next()
	err = rows.Err()
	rows.Close()
	return !violations, err
}
