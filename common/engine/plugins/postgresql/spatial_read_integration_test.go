package postgresql

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/twpayne/go-geom/encoding/ewkb"
)

func TestIntegrationPostgresReadBatchHonorsSpatialEncoding(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, true)
	defer db.Close()

	ctx := context.Background()
	schema := "common_pg_it"
	table := fmt.Sprintf("spatial_read_%d", time.Now().UnixNano())
	createPostgresPrepareBaseTable(t, ctx, db, schema, table, `"id" bigint PRIMARY KEY, "geometry" geometry(Point,4326) NOT NULL`)
	defer dropPostgresPrepareTable(db, schema, table)
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO "%s"."%s" (id, geometry) VALUES (1, ST_SetSRID(ST_MakePoint(121.5, 31.2), 4326)), (2, ST_SetSRID(ST_MakePoint(116.4, 39.9), 4326))`, schema, table)); err != nil {
		t.Fatalf("insert spatial rows: %v", err)
	}

	path := postgresPrepareTablePath(schema, table)
	geoJSONBatch, err := pg.ReadBatch(ctx, connInfo, path, plugin.BatchReadOptions{
		Limit:  1,
		Offset: 1,
		Hints: map[string]interface{}{
			plugin.TableReadHintGeometryEncoding: string(format.GeometryEncodingGeoJSON),
		},
	})
	if err != nil {
		t.Fatalf("ReadBatch GeoJSON failed: %v", err)
	}
	if len(geoJSONBatch.Rows) != 1 || geoJSONBatch.Offset != 1 {
		t.Fatalf("GeoJSON batch = %#v, want one row at offset 1", geoJSONBatch)
	}
	geometryJSON, ok := geoJSONBatch.Rows[0]["geometry"].(string)
	if !ok {
		t.Fatalf("GeoJSON geometry value = %T, want string", geoJSONBatch.Rows[0]["geometry"])
	}
	var geometry map[string]interface{}
	if err := json.Unmarshal([]byte(geometryJSON), &geometry); err != nil {
		t.Fatalf("decode GeoJSON geometry: %v", err)
	}
	if geometry["type"] != "Point" {
		t.Fatalf("GeoJSON geometry = %#v, want Point", geometry)
	}

	ewkbBatch, err := pg.ReadBatch(ctx, connInfo, path, plugin.BatchReadOptions{
		Limit: 1,
		Hints: map[string]interface{}{
			plugin.TableReadHintGeometryEncoding: string(format.GeometryEncodingEWKB),
		},
	})
	if err != nil {
		t.Fatalf("ReadBatch EWKB failed: %v", err)
	}
	encoded, ok := ewkbBatch.Rows[0]["geometry"].([]byte)
	if !ok {
		t.Fatalf("EWKB geometry value = %T, want []byte", ewkbBatch.Rows[0]["geometry"])
	}
	decoded, err := ewkb.Unmarshal(encoded)
	if err != nil {
		t.Fatalf("decode EWKB geometry: %v", err)
	}
	if decoded.SRID() != 4326 {
		t.Fatalf("EWKB SRID = %d, want 4326", decoded.SRID())
	}

	defaultLimitBatch, err := pg.ReadBatch(ctx, connInfo, path, plugin.BatchReadOptions{
		Offset: 1,
		Hints: map[string]interface{}{
			plugin.TableReadHintGeometryEncoding: string(format.GeometryEncodingGeoJSON),
		},
	})
	if err != nil {
		t.Fatalf("ReadBatch default limit failed: %v", err)
	}
	if len(defaultLimitBatch.Rows) != 1 || defaultLimitBatch.Offset != 1 {
		t.Fatalf("default-limit batch = %#v, want the remaining row at offset 1", defaultLimitBatch)
	}

	parameterizedBatch, err := pg.ReadBatch(ctx, connInfo, path, plugin.BatchReadOptions{
		Query:  fmt.Sprintf(`SELECT "id" FROM "%s"."%s" WHERE "id" > $1 ORDER BY "id"`, schema, table),
		Args:   []interface{}{int64(0)},
		Limit:  1,
		Offset: 1,
	})
	if err != nil {
		t.Fatalf("ReadBatch parameterized query failed: %v", err)
	}
	if len(parameterizedBatch.Rows) != 1 || parameterizedBatch.Rows[0]["id"] != int64(2) {
		t.Fatalf("parameterized batch = %#v, want id 2", parameterizedBatch)
	}
}
