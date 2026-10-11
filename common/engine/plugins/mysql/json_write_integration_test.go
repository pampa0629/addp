package mysql

import (
	"context"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugins/shared/writetest"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
)

func TestIntegrationMySQLTypedJSONWrites(t *testing.T) {
	db, p, connInfo, database := openMySQLUpsertIntegration(t)
	t.Cleanup(func() { _ = db.Close() })
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := db.ExecContext(ctx, "DROP DATABASE IF EXISTS "+mysqlDialect().QuoteIdentifier(database)); err != nil {
			t.Errorf("cleanup JSON database: %v", err)
		}
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name=?", database).Scan(&count); err != nil || count != 0 {
			t.Errorf("JSON database cleanup not proven: count=%d error=%v", count, err)
		}
	})
	t.Run("non_spatial", func(t *testing.T) { writetest.MySQLJSONWrites(t, db, p, connInfo, database, nil, nil) })
	t.Run("spatial", func(t *testing.T) {
		srid := 4326
		spatial := &datatype.SpatialInfo{GeometryColumns: []datatype.GeometryColumnInfo{{Name: "shape", GeometryType: "Point", SRID: &srid, CRSRef: "EPSG:4326"}}}
		geometry, err := ewkb.Marshal(geom.NewPointFlat(geom.XY, []float64{1, 2}).SetSRID(srid), ewkb.NDR)
		if err != nil {
			t.Fatal(err)
		}
		writetest.MySQLJSONWrites(t, db, p, connInfo, database, spatial, geometry)
	})
}
