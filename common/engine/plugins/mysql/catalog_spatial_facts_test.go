package mysql

import (
	"database/sql"
	"testing"

	"github.com/addp/common/datatype"
)

func TestBuildMySQLSpatialInfoMapsAllFacts(t *testing.T) {
	info := buildMySQLSpatialInfo([]mysqlSpatialColumnRow{
		{Name: "center", DataType: "point", SRSID: sql.NullInt64{Int64: 4326, Valid: true}},
		{Name: "parts", DataType: "geomcollection", SRSID: sql.NullInt64{Int64: 4326, Valid: true}, Nullable: true},
	}, []mysqlSpatialIndexRow{{Name: "idx_center", ColumnName: "center"}}, map[int]datatype.CRSDefinition{
		4326: {ID: "EPSG:4326", DefinitionEncoding: datatype.CRSDefinitionEncodingWKT, Definition: "GEOGCS[...]", Source: datatype.CRSDefinitionSourceMySQLSpatialRefSys},
	})

	if info == nil || info.PrimaryGeometryColumn != "center" || len(info.GeometryColumns) != 2 {
		t.Fatalf("spatial info = %#v", info)
	}
	if info.GeometryColumns[0].GeometryType != "Point" || info.GeometryColumns[1].GeometryType != "GeometryCollection" {
		t.Fatalf("geometry columns = %#v", info.GeometryColumns)
	}
	if info.GeometryColumns[0].SRID == nil || *info.GeometryColumns[0].SRID != 4326 || info.GeometryColumns[0].CRSRef != "EPSG:4326" {
		t.Fatalf("primary geometry CRS = %#v", info.GeometryColumns[0])
	}
	if info.GeometryColumns[1].Nullable == nil || !*info.GeometryColumns[1].Nullable {
		t.Fatalf("nullable geometry = %#v", info.GeometryColumns[1])
	}
	if info.HasSpatialIndex == nil || !*info.HasSpatialIndex || info.IndexName != "idx_center" {
		t.Fatalf("spatial index = %#v", info)
	}
}
