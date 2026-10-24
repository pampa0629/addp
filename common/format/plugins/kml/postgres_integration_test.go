package kml_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/xml"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/kml"
	"github.com/lib/pq"
	"github.com/twpayne/go-geom/encoding/ewkb"
)

func TestIntegrationPostgresKMLWriter(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard Common PostgreSQL integration gate")
	}
	ctx := t.Context()
	info := plugin.ConnectionInfo{}
	for _, key := range []string{"host", "port", "user", "password", "database", "sslmode"} {
		info[key] = os.Getenv("ADDP_TEST_POSTGRES_" + strings.ToUpper(key))
	}
	pg := &postgresql.PostgreSQLPlugin{}
	dsn, err := pg.BuildDSN(info)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.PingContext(ctx); err != nil {
		t.Fatal("PostgreSQL unavailable")
	}
	schema := fmt.Sprintf("addp_kml_export_%d", time.Now().UnixNano())
	quoted := pq.QuoteIdentifier(schema)
	if _, err = db.ExecContext(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := db.ExecContext(cleanup, "DROP TABLE IF EXISTS "+quoted+".points RESTRICT; DROP SCHEMA "+quoted+" RESTRICT"); err != nil {
			t.Errorf("KML fixture cleanup: %v", err)
		}
		var remains bool
		if err := db.QueryRowContext(cleanup, "SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname=$1)", schema).Scan(&remains); err != nil || remains {
			t.Errorf("KML schema cleanup: remains=%v error=%v", remains, err)
		}
	}()
	if _, err = db.ExecContext(ctx, "CREATE TABLE "+quoted+`.points (seq integer PRIMARY KEY, label text, route_shape geometry(Point,3857) NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "INSERT INTO "+quoted+`.points VALUES (1, '中文 <&>', ST_Transform(ST_SetSRID(ST_MakePoint(10,0),4326),3857)), (2, NULL, ST_Transform(ST_SetSRID(ST_MakePoint(20,10),4326),3857))`); err != nil {
		t.Fatal(err)
	}
	path := plugin.EngineCatalogPath{Version: plugin.EngineCatalogPathVersion, Segments: []plugin.EngineCatalogSegment{{Term: plugin.EngineCatalogTermSchema, Kind: plugin.EngineCatalogKindNamespace, Name: schema}, {Term: plugin.EngineCatalogTermTable, Kind: plugin.EngineCatalogKindTable, Name: "points"}}}
	session, err := pg.OpenTableReadSession(ctx, info, path, plugin.TableReadSessionOptions{Hints: map[string]interface{}{plugin.TableReadHintGeometryEncoding: "ewkb", plugin.TableReadHintGeometryField: "route_shape", plugin.TableReadHintGeometryTargetSRID: 4326, plugin.TableReadHintGeometryTransformPolicy: "required"}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(ctx)
	var output bytes.Buffer
	writer, err := kml.NewPlugin().OpenTableWriter(ctx, &output, &datatype.TableInfo{Name: "native", Fields: []datatype.FieldInfo{{Name: "seq", Type: datatype.FieldTypeInt}, {Name: "label", Type: datatype.FieldTypeString, Nullable: true}, {Name: "route_shape", Type: datatype.FieldTypeGeometry}}}, &format.WriteOptions{SpatialInfo: datatype.NewSingleGeometrySpatialInfo("route_shape", "Point", 4326, 2)})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for {
		batch, err := session.ReadBatch(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		if batch == nil || len(batch.Rows) == 0 {
			break
		}
		if len(batch.Rows) != 1 {
			t.Fatal("source session ignored limit")
		}
		data, ok := batch.Rows[0]["route_shape"].([]byte)
		if !ok {
			t.Fatalf("not EWKB: %T", batch.Rows[0]["route_shape"])
		}
		g, err := ewkb.Unmarshal(data)
		if err != nil || g.SRID() != 4326 {
			t.Fatalf("source did not transform: %v %v", g, err)
		}
		if err = writer.WriteRows(ctx, batch.Rows); err != nil {
			t.Fatal(err)
		}
		count++
	}
	if err = writer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Document struct {
			Folder struct {
				Rows []struct {
					Point struct {
						Coordinates string `xml:"coordinates"`
					} `xml:"Point"`
				} `xml:"Placemark"`
			} `xml:"Folder"`
		} `xml:"Document"`
	}
	if err = xml.Unmarshal(output.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if count != 2 || len(doc.Document.Folder.Rows) != 2 || !strings.Contains(output.String(), "中文 &lt;&amp;&gt;") {
		t.Fatal(output.String())
	}
	for i, row := range doc.Document.Folder.Rows {
		var x, y float64
		if _, err = fmt.Sscanf(row.Point.Coordinates, "%f,%f", &x, &y); err != nil || math.Abs(x-float64((i+1)*10)) > 1e-9 || math.Abs(y-float64(i*10)) > 1e-9 {
			t.Fatalf("coordinates=%q error=%v", row.Point.Coordinates, err)
		}
	}
}
