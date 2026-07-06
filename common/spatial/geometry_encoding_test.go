package spatial_test

import (
	"encoding/hex"
	"testing"

	"github.com/addp/common/spatial"
	"github.com/twpayne/go-geom"
)

func TestGeometryEncodingRoundTripWKTWKBAndEWKB(t *testing.T) {
	t.Parallel()

	point := geom.NewPointFlat(geom.XY, []float64{116.4, 39.9})
	wktText, err := spatial.GeomToWKT(point)
	if err != nil {
		t.Fatalf("GeomToWKT() error = %v", err)
	}
	if wktText != "POINT (116.4 39.9)" {
		t.Fatalf("GeomToWKT() = %q", wktText)
	}

	wkbBytes, err := spatial.GeomToWKB(point)
	if err != nil {
		t.Fatalf("GeomToWKB() error = %v", err)
	}
	fromWKB, err := spatial.ParseGeometryValue(wkbBytes)
	if err != nil {
		t.Fatalf("ParseGeometryValue(WKB) error = %v", err)
	}
	if got := fromWKB.(*geom.Point).X(); got != 116.4 {
		t.Fatalf("WKB X = %v", got)
	}

	ewkbBytes, err := spatial.GeomToEWKB(point, 4326)
	if err != nil {
		t.Fatalf("GeomToEWKB() error = %v", err)
	}
	fromEWKB, err := spatial.ParseGeometryValue("0x" + hex.EncodeToString(ewkbBytes))
	if err != nil {
		t.Fatalf("ParseGeometryValue(EWKB hex) error = %v", err)
	}
	if got := fromEWKB.SRID(); got != 4326 {
		t.Fatalf("EWKB SRID = %d, want 4326", got)
	}
	if got := point.SRID(); got != 0 {
		t.Fatalf("GeomToEWKB mutated source SRID to %d", got)
	}
}

func TestEncodeGeometryBytesAsEWKBEmbedsSRID(t *testing.T) {
	point := geom.NewPointFlat(geom.XY, []float64{120, 30})
	wkbBytes, err := spatial.GeomToWKB(point)
	if err != nil {
		t.Fatalf("GeomToWKB() error = %v", err)
	}

	encoded, err := spatial.EncodeGeometryBytesAsEWKB([][]byte{wkbBytes, nil}, 4326)
	if err != nil {
		t.Fatalf("EncodeGeometryBytesAsEWKB() error = %v", err)
	}
	if len(encoded) != 2 {
		t.Fatalf("encoded length = %d, want 2", len(encoded))
	}
	if encoded[1] != nil {
		t.Fatalf("encoded[1] = %#v, want nil", encoded[1])
	}
	geometry, err := spatial.ParseGeometryValue(encoded[0])
	if err != nil {
		t.Fatalf("ParseGeometryValue(EWKB) error = %v", err)
	}
	if geometry.SRID() != 4326 {
		t.Fatalf("SRID = %d, want 4326", geometry.SRID())
	}
}

func TestGeoJSONGeometryObjectRoundTrip(t *testing.T) {
	t.Parallel()

	source := map[string]interface{}{
		"type":        "Point",
		"coordinates": []interface{}{116.4, 39.9},
	}
	geometry, err := spatial.GeoJSONGeometryToGeom(source, 4326)
	if err != nil {
		t.Fatalf("GeoJSONGeometryToGeom() error = %v", err)
	}
	if got := geometry.SRID(); got != 4326 {
		t.Fatalf("SRID = %d, want 4326", got)
	}
	encoded, err := spatial.GeomToEWKB(geometry, geometry.SRID())
	if err != nil {
		t.Fatalf("GeomToEWKB() error = %v", err)
	}
	decoded, err := spatial.ParseGeometryValue(encoded)
	if err != nil {
		t.Fatalf("ParseGeometryValue(EWKB) error = %v", err)
	}
	geojson, err := spatial.GeomToGeoJSONGeometry(decoded)
	if err != nil {
		t.Fatalf("GeomToGeoJSONGeometry() error = %v", err)
	}
	if got := geojson["type"]; got != "Point" {
		t.Fatalf("GeoJSON type = %#v, want Point", got)
	}
}

func TestDecodeEncodeGeometryValueWithStandardEncodings(t *testing.T) {
	t.Parallel()

	point := geom.NewPointFlat(geom.XY, []float64{116.4, 39.9})

	wktValue, err := spatial.EncodeGeometryValue(point, "wkt", 0)
	if err != nil {
		t.Fatalf("EncodeGeometryValue(wkt) error = %v", err)
	}
	if wktValue != "POINT (116.4 39.9)" {
		t.Fatalf("WKT value = %#v", wktValue)
	}
	fromWKT, err := spatial.DecodeGeometryValue(wktValue, "wkt", 4326)
	if err != nil {
		t.Fatalf("DecodeGeometryValue(wkt) error = %v", err)
	}
	if got := fromWKT.SRID(); got != 4326 {
		t.Fatalf("WKT decoded SRID = %d, want 4326", got)
	}

	wkbValue, err := spatial.EncodeGeometryValue(point, "wkb", 4326)
	if err != nil {
		t.Fatalf("EncodeGeometryValue(wkb) error = %v", err)
	}
	fromWKB, err := spatial.DecodeGeometryValue(wkbValue, "wkb", 3857)
	if err != nil {
		t.Fatalf("DecodeGeometryValue(wkb) error = %v", err)
	}
	if got := fromWKB.SRID(); got != 3857 {
		t.Fatalf("WKB decoded SRID = %d, want 3857", got)
	}

	ewkbValue, err := spatial.EncodeGeometryValue(point, "ewkb", 4326)
	if err != nil {
		t.Fatalf("EncodeGeometryValue(ewkb) error = %v", err)
	}
	fromEWKB, err := spatial.DecodeGeometryValue(ewkbValue, "ewkb", 0)
	if err != nil {
		t.Fatalf("DecodeGeometryValue(ewkb) error = %v", err)
	}
	if got := fromEWKB.SRID(); got != 4326 {
		t.Fatalf("EWKB decoded SRID = %d, want 4326", got)
	}

	geojsonValue, err := spatial.EncodeGeometryValue(point, "geojson_geometry", 4326)
	if err != nil {
		t.Fatalf("EncodeGeometryValue(geojson) error = %v", err)
	}
	fromGeoJSON, err := spatial.DecodeGeometryValue(geojsonValue, "geojson", 4326)
	if err != nil {
		t.Fatalf("DecodeGeometryValue(geojson) error = %v", err)
	}
	if got := fromGeoJSON.SRID(); got != 4326 {
		t.Fatalf("GeoJSON decoded SRID = %d, want 4326", got)
	}
	if got := point.SRID(); got != 0 {
		t.Fatalf("EncodeGeometryValue mutated source SRID to %d", got)
	}
}

func TestDecodeGeometryValueRejectsNativeEncoding(t *testing.T) {
	t.Parallel()

	if got := spatial.NormalizeGeometryEncoding("shapefile_shape"); got != "" {
		t.Fatalf("NormalizeGeometryEncoding(shapefile_shape) = %q, want empty", got)
	}
	if _, err := spatial.DecodeGeometryValue("POINT (1 2)", "shapefile_shape", 0); err == nil {
		t.Fatal("DecodeGeometryValue accepted native shapefile_shape encoding")
	}
}
