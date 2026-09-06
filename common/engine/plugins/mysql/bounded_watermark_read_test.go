package mysql

import (
	"database/sql"
	"testing"

	"github.com/addp/common/datatype"
)

func TestMySQLCapabilitiesDeclareBoundedWatermarkRead(t *testing.T) {
	capabilities := (&MySQLPlugin{}).Capabilities()
	if capabilities.Storage == nil || capabilities.Storage.Store == nil || !capabilities.Storage.Store.BoundedWatermarkRead {
		t.Fatalf("MySQL bounded watermark read capability = %#v, want true", capabilities.Storage)
	}
}

func TestMySQLWatermarkFieldsDescribeSpatialColumns(t *testing.T) {
	fields, spatialInfo, err := mysqlWatermarkFields([]mysqlColumnInfo{
		{Name: "id", NativeType: "bigint"},
		{Name: "shape", DataType: "point", NativeType: "point", SRSID: sqlNullInt64(4326)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 || fields[1].Type != datatype.FieldTypeGeometry {
		t.Fatalf("fields = %#v", fields)
	}
	if spatialInfo == nil || spatialInfo.PrimaryGeometryName() != "shape" || spatialInfo.PrimarySRIDValue() != 4326 {
		t.Fatalf("spatial info = %#v", spatialInfo)
	}
}

func sqlNullInt64(value int64) sql.NullInt64 {
	return sql.NullInt64{Int64: value, Valid: true}
}
