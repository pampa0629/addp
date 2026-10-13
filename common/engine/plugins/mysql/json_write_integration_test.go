package mysql

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
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
		t.Run("invalid_mixed_definition_before_ddl", func(t *testing.T) {
			for _, scenario := range []string{"decimal", "dimension"} {
				t.Run(scenario, func(t *testing.T) {
					table := fmt.Sprintf("invalid_mixed_%s_%d", scenario, time.Now().UnixNano())
					qualified := mysqlDialect().QualifiedTable(database, table)
					path := mysqlIntegrationTablePath(database, table)
					fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}, {Name: "shape", Type: datatype.FieldTypeGeometry}}
					sp := spatial.Clone()
					if scenario == "decimal" {
						fields = append(fields, datatype.FieldInfo{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 2, Scale: 3})
					} else {
						dimension := 3
						sp.GeometryColumns[0].Dimension = &dimension
					}
					t.Cleanup(func() {
						if _, err := db.Exec("DROP TABLE IF EXISTS " + qualified); err != nil {
							t.Errorf("cleanup rejected definition: %v", err)
						}
					})
					for _, err := range []error{
						p.PrepareTableWrite(t.Context(), connInfo, path, plugin.TableWriteOptions{Fields: fields, SpatialInfo: sp}),
						p.PrepareTableUpsert(t.Context(), connInfo, path, plugin.TableUpsertOptions{Fields: fields, SpatialInfo: sp, Keys: []string{"id"}}),
					} {
						if err == nil || !strings.Contains(err.Error(), "target_definition") {
							t.Fatalf("invalid definition reached DDL: %v", err)
						}
					}
					var count int
					if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=? AND table_name=?", database, table).Scan(&count); err != nil || count != 0 {
						t.Fatalf("rejected definition created table: count=%d error=%v", count, err)
					}
				})
			}
		})
		t.Run("existing_polygon_rejects_point_definition", func(t *testing.T) {
			table := fmt.Sprintf("spatial_subtype_%d", time.Now().UnixNano())
			qualified := mysqlDialect().QualifiedTable(database, table)
			if _, err := db.Exec("CREATE TABLE " + qualified + " (id BIGINT NOT NULL PRIMARY KEY, shape POLYGON SRID 4326)"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := db.Exec("DROP TABLE IF EXISTS " + qualified); err != nil {
					t.Errorf("cleanup subtype table: %v", err)
				}
			})
			if _, err := db.Exec("INSERT INTO " + qualified + " (id) VALUES (1)"); err != nil {
				t.Fatal(err)
			}
			err := p.PrepareTableWrite(t.Context(), connInfo, mysqlIntegrationTablePath(database, table), plugin.TableWriteOptions{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}, {Name: "shape", Type: datatype.FieldTypeGeometry, Nullable: true}}, SpatialInfo: spatial})
			if err == nil || !strings.Contains(err.Error(), `column "shape"`) {
				t.Fatalf("POINT definition accepted existing POLYGON: %v", err)
			}
			var native string
			var count int
			if err := db.QueryRow("SELECT data_type FROM information_schema.columns WHERE table_schema=? AND table_name=? AND column_name='shape'", database, table).Scan(&native); err != nil || native != "polygon" {
				t.Fatalf("target type changed: %s error=%v", native, err)
			}
			if err := db.QueryRow("SELECT COUNT(*) FROM " + qualified).Scan(&count); err != nil || count != 1 {
				t.Fatalf("sentinel changed: count=%d error=%v", count, err)
			}
		})
	})
}
