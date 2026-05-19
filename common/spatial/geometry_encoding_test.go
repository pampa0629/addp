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
