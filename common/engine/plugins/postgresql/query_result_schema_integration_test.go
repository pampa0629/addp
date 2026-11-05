package postgresql

import (
	"bytes"
	"github.com/twpayne/go-geom/encoding/ewkb"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestIntegrationPostgresCatalogPreciseReadOnlyOutputSchema(t *testing.T) {
	db, provider, info := openPostgresPrepareIntegration(t, false)
	t.Cleanup(func() { db.Close() })
	result, err := provider.ExecuteSQL(t.Context(), info, `SELECT 2::integer AS small, 2::bigint AS large, 1.25::numeric AS unbounded, 1.25::numeric(38,18) AS fixed`, plugin.QueryOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Fields) != 4 || result.Fields[0].Type != datatype.FieldTypeInt || result.Fields[1].Type != datatype.FieldTypeBigInt || result.Fields[2].Type != datatype.FieldTypeDecimal || result.Fields[2].Precision != 0 || result.Fields[2].Scale != 0 || result.Fields[3].Precision != 38 || result.Fields[3].Scale != 18 {
		t.Fatalf("native output schema: %+v", result.Fields)
	}
}

func TestIntegrationPostgresQueryReadSessionGeometry(t *testing.T) {
	db, provider, info := openPostgresPrepareIntegration(t, true)
	defer db.Close()
	query := `SELECT ST_SetSRID(ST_MakePoint(1,2,3),32650) AS shape_alias, ST_GeomFromText('POLYGON EMPTY',4326) AS empty_shape, NULL::geometry AS absent, decode('00ff80','hex') AS payload, ST_SetSRID(ST_MakePoint(1,2),4326)::geography AS location, point(1,2) AS native_point`
	result, err := provider.ExecuteSQL(t.Context(), info, query, plugin.QueryOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fields[0].Type != datatype.FieldTypeGeometry || result.Fields[4].Type != datatype.FieldTypeGeometry || result.Fields[5].Type == datatype.FieldTypeGeometry {
		t.Fatalf("PostGIS output schema: %+v", result.Fields)
	}
	prepared, err := provider.PrepareQuery(t.Context(), info, plugin.QueryRequest{Language: "sql", Query: query, Options: plugin.QueryOptions{ReadOnly: true}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := provider.OpenQueryReadSession(t.Context(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(t.Context())
	batch, err := session.ReadBatch(t.Context(), 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"shape_alias", "empty_shape", "location"} {
		raw, ok := result.Rows[0][name].([]byte)
		if !ok {
			t.Fatalf("non-binary geometry %T", result.Rows[0][name])
		}
		if !bytes.Equal(raw, batch.Rows[0][name].([]byte)) {
			t.Fatal("preview and cursor geometry differ")
		}
		if _, err := ewkb.Unmarshal(raw); err != nil {
			t.Fatal(err)
		}
	}
	if result.Rows[0]["absent"] != nil || !bytes.Equal(result.Rows[0]["payload"].([]byte), []byte{0, 255, 128}) {
		t.Fatal("NULL or BYTEA changed")
	}
	wire, err := datatype.QueryRowsForJSON(result.Fields, result.Rows)
	if err != nil {
		t.Fatal(err)
	}
	shape := wire[0]["shape_alias"].(datatype.GeometryValue)
	if shape.WKT != "POINT Z (1 2 3)" || shape.SRID != 32650 || wire[0]["empty_shape"].(datatype.GeometryValue).WKT != "POLYGON EMPTY" {
		t.Fatalf("geometry projection lost facts: %+v", wire)
	}
}
