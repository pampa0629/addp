package oracle

import (
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
	"github.com/twpayne/go-geom/encoding/wkb"
)

func TestOraclePartitionedApplyOptionsAndTypes(t *testing.T) {
	opts := plugin.PartitionedTableChangeApplyOptions{
		ApplyIdentity:  "eef1d731-ea11-449d-8a55-c930d75db0c2",
		SourceIdentity: "addp://engine/22/path/BUSINESS/ORDERS?type=table",
		Fields: []datatype.FieldInfo{
			{Name: "ID", Type: datatype.FieldTypeBigInt},
			{Name: "AMOUNT", Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2},
		},
		Keys: []string{"ID"},
	}
	keys, err := validateOraclePartitionedTableChangeApplyOptions(opts)
	if err != nil || strings.Join(keys, ",") != "ID" {
		t.Fatalf("validate options keys=%v err=%v", keys, err)
	}
	if got, err := oracleSQLTypeForField(opts.Fields[1]); err != nil || got != "NUMBER(18,2)" {
		t.Fatalf("decimal SQL type=%q err=%v", got, err)
	}
	for _, field := range []datatype.FieldInfo{
		{Name: "CLOCK", Type: datatype.FieldTypeTime},
		{Name: "TOO_WIDE", Type: datatype.FieldTypeDecimal, Precision: 39, Scale: 2},
	} {
		if _, err := oracleSQLTypeForField(field); err == nil {
			t.Fatalf("oracleSQLTypeForField(%#v) unexpectedly succeeded", field)
		}
	}
}

func TestOracleGeometryWKBValidatesFrozenFacts(t *testing.T) {
	srid, dimension := 4326, 2
	column := &datatype.GeometryColumnInfo{
		Name: "SHAPE", GeometryType: string(datatype.GeometryTypePoint),
		SRID: &srid, Dimension: &dimension,
	}
	point := geom.NewPointFlat(geom.XY, []float64{116.4, 39.9}).SetSRID(4326)
	encoded, err := ewkb.Marshal(point, ewkb.NDR)
	if err != nil {
		t.Fatal(err)
	}
	standard, err := oracleGeometryWKB(encoded, column)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := wkb.Unmarshal(standard)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.SRID() != 0 || decoded.Layout() != geom.XY {
		t.Fatalf("standard WKB geometry layout=%s srid=%d", decoded.Layout(), decoded.SRID())
	}
	wrongSRID := *column
	value := 3857
	wrongSRID.SRID = &value
	if _, err := oracleGeometryWKB(encoded, &wrongSRID); err == nil || !strings.Contains(err.Error(), "SRID") {
		t.Fatalf("wrong SRID error=%v", err)
	}
	wrongType := *column
	wrongType.GeometryType = string(datatype.GeometryTypePolygon)
	if _, err := oracleGeometryWKB(encoded, &wrongType); err == nil || !strings.Contains(err.Error(), "type") {
		t.Fatalf("wrong type error=%v", err)
	}
	wrongDimension := *column
	value = 3
	wrongDimension.Dimension = &value
	if _, err := oracleGeometryWKB(encoded, &wrongDimension); err == nil || !strings.Contains(err.Error(), "dimension") {
		t.Fatalf("wrong dimension error=%v", err)
	}
}

func TestOracleApplyValueExpressionUsesNativeSpatialConstructor(t *testing.T) {
	srid, dimension := 4326, 2
	point := geom.NewPointFlat(geom.XY, []float64{116.4, 39.9}).SetSRID(srid)
	encoded, err := ewkb.Marshal(point, ewkb.NDR)
	if err != nil {
		t.Fatal(err)
	}
	expression, args, err := oracleApplyValueExpression(
		datatype.FieldInfo{Name: "SHAPE", Type: datatype.FieldTypeGeometry},
		encoded,
		&datatype.SpatialInfo{GeometryColumns: []datatype.GeometryColumnInfo{{
			Name: "SHAPE", GeometryType: string(datatype.GeometryTypePoint), SRID: &srid, Dimension: &dimension,
		}}},
		3,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"SDO_UTIL.FROM_WKBGEOMETRY(:3)", "MDSYS.SDO_GEOMETRY", "2001", "4326"} {
		if !strings.Contains(expression, expected) {
			t.Fatalf("spatial expression=%q missing %q", expression, expected)
		}
	}
	if len(args) != 1 {
		t.Fatalf("spatial args=%#v", args)
	}
}
