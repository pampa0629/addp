package integration_test

import (
	"github.com/addp/common/datatype"
	"testing"

	"github.com/addp/common/format"

	// 导入内置类型映射器
	_ "github.com/addp/common/format/builtin"
)

func TestPostgreSQLTypeMapperToCommon(t *testing.T) {
	mapper := format.GetTypeMapper("postgresql")
	if mapper == nil {
		t.Fatal("postgresql type mapper is not registered")
	}

	tests := []struct {
		pgType string
		want   datatype.FieldType
	}{
		{"varchar", datatype.FieldTypeString},
		{"varchar(255)", datatype.FieldTypeString},
		{"text", datatype.FieldTypeString},
		{"character varying", datatype.FieldTypeString},
		{"smallint", datatype.FieldTypeInt},
		{"integer", datatype.FieldTypeInt},
		{"int", datatype.FieldTypeInt},
		{"bigint", datatype.FieldTypeBigInt},
		{"real", datatype.FieldTypeFloat},              // 4字节单精度
		{"double precision", datatype.FieldTypeDouble}, // 8字节双精度
		{"numeric", datatype.FieldTypeDecimal},
		{"numeric(10,2)", datatype.FieldTypeDecimal},
		{"boolean", datatype.FieldTypeBool},
		{"bool", datatype.FieldTypeBool},
		{"date", datatype.FieldTypeDate},
		{"time", datatype.FieldTypeTime},
		{"timestamp", datatype.FieldTypeTimestamp},
		{"timestamp with time zone", datatype.FieldTypeTimestamp},
		{"geometry", datatype.FieldTypeGeometry},
		{"point", datatype.FieldTypePoint},
		{"linestring", datatype.FieldTypeLineString},
		{"polygon", datatype.FieldTypePolygon},
		{"json", datatype.FieldTypeJSON},
		{"jsonb", datatype.FieldTypeJSON},
		{"uuid", datatype.FieldTypeUUID},
		{"integer[]", datatype.FieldTypeArray},
		{"text[]", datatype.FieldTypeArray},
		{"custom_type", datatype.FieldTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.pgType, func(t *testing.T) {
			got := mapper.ToCommon(tt.pgType)
			if got != tt.want {
				t.Errorf("PostgreSQLToCommon(%q) = %v, want %v", tt.pgType, got, tt.want)
			}
		})
	}
}

func TestMySQLTypeMapperToCommon(t *testing.T) {
	mapper := format.GetTypeMapper("mysql")
	if mapper == nil {
		t.Fatal("mysql type mapper is not registered")
	}

	tests := []struct {
		mysqlType string
		want      datatype.FieldType
	}{
		{"varchar", datatype.FieldTypeString},
		{"int", datatype.FieldTypeInt},
		{"bigint", datatype.FieldTypeBigInt},
		{"float", datatype.FieldTypeFloat},   // 4字节单精度
		{"double", datatype.FieldTypeDouble}, // 8字节双精度
		{"decimal", datatype.FieldTypeDecimal},
		{"datetime", datatype.FieldTypeTimestamp},
		{"json", datatype.FieldTypeJSON},
	}

	for _, tt := range tests {
		t.Run(tt.mysqlType, func(t *testing.T) {
			got := mapper.ToCommon(tt.mysqlType)
			if got != tt.want {
				t.Errorf("MySQLToCommon(%q) = %v, want %v", tt.mysqlType, got, tt.want)
			}
		})
	}
}

func TestSpatiaLiteTypeMapperToCommon(t *testing.T) {
	mapper := format.GetTypeMapper("spatialite")
	if mapper == nil {
		t.Fatal("spatialite type mapper is not registered")
	}

	tests := []struct {
		sqliteType string
		want       datatype.FieldType
	}{
		{"text", datatype.FieldTypeString},
		{"varchar(255)", datatype.FieldTypeString},
		{"integer", datatype.FieldTypeInt},
		{"bigint", datatype.FieldTypeInt},
		{"real", datatype.FieldTypeDouble},
		{"boolean", datatype.FieldTypeBool},
		{"datetime", datatype.FieldTypeTimestamp},
		{"blob", datatype.FieldTypeBytes},
		{"geometry", datatype.FieldTypeGeometry},
		{"point", datatype.FieldTypePoint},
		{"linestring", datatype.FieldTypeLineString},
		{"polygon", datatype.FieldTypePolygon},
		{"multipoint", datatype.FieldTypeMultiPoint},
		{"custom_type", datatype.FieldTypeString},
	}

	for _, tt := range tests {
		t.Run(tt.sqliteType, func(t *testing.T) {
			got := mapper.ToCommon(tt.sqliteType)
			if got != tt.want {
				t.Errorf("SpatiaLiteToCommon(%q) = %v, want %v", tt.sqliteType, got, tt.want)
			}
		})
	}
}

func TestShapefileTypeMapperDBFToCommon(t *testing.T) {
	mapper := format.GetTypeMapper("shapefile")
	if mapper == nil {
		t.Fatal("shapefile type mapper is not registered")
	}

	tests := []struct {
		dbfType byte
		want    datatype.FieldType
	}{
		{'C', datatype.FieldTypeString},
		{'N', datatype.FieldTypeFloat},
		{'F', datatype.FieldTypeFloat},
		{'L', datatype.FieldTypeBool},
		{'D', datatype.FieldTypeDate},
		{'M', datatype.FieldTypeString},
		{'X', datatype.FieldTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(string(tt.dbfType), func(t *testing.T) {
			got := mapper.ToCommon(string(tt.dbfType))
			if got != tt.want {
				t.Errorf("ShapefileDBFToCommon(%q) = %v, want %v", tt.dbfType, got, tt.want)
			}
		})
	}
}

func TestPostgreSQLTypeMapperFromCommon(t *testing.T) {
	mapper := format.GetTypeMapper("postgresql")
	if mapper == nil {
		t.Fatal("postgresql type mapper is not registered")
	}

	tests := []struct {
		commonType datatype.FieldType
		want       string
	}{
		{datatype.FieldTypeString, "TEXT"},
		{datatype.FieldTypeInt, "INTEGER"},
		{datatype.FieldTypeBigInt, "BIGINT"},
		{datatype.FieldTypeFloat, "REAL"},              // 4字节单精度
		{datatype.FieldTypeDouble, "DOUBLE PRECISION"}, // 8字节双精度
		{datatype.FieldTypeDecimal, "NUMERIC"},
		{datatype.FieldTypeBool, "BOOLEAN"},
		{datatype.FieldTypeDate, "DATE"},
		{datatype.FieldTypeTimestamp, "TIMESTAMP"},
		{datatype.FieldTypeGeometry, "GEOMETRY"},
		{datatype.FieldTypePoint, "GEOMETRY(Point)"},
		{datatype.FieldTypeJSON, "JSONB"},
		{datatype.FieldTypeUUID, "UUID"},
	}

	for _, tt := range tests {
		t.Run(string(tt.commonType), func(t *testing.T) {
			got, _, _ := mapper.FromCommon(tt.commonType)
			if got != tt.want {
				t.Errorf("CommonToPostgreSQL(%v) = %q, want %q", tt.commonType, got, tt.want)
			}
		})
	}
}

func TestShapefileTypeMapperFromCommon(t *testing.T) {
	mapper := format.GetTypeMapper("shapefile")
	if mapper == nil {
		t.Fatal("shapefile type mapper is not registered")
	}

	tests := []struct {
		commonType datatype.FieldType
		wantType   byte
		wantSize   uint8
		wantPrec   uint8
	}{
		{datatype.FieldTypeString, 'C', 254, 0},
		{datatype.FieldTypeInt, 'N', 18, 0},
		{datatype.FieldTypeBigInt, 'N', 18, 0},
		{datatype.FieldTypeFloat, 'F', 13, 6},   // 单精度
		{datatype.FieldTypeDouble, 'F', 20, 8},  // 双精度
		{datatype.FieldTypeDecimal, 'N', 20, 8}, // 高精度小数用 Numeric
		{datatype.FieldTypeBool, 'L', 1, 0},
		{datatype.FieldTypeDate, 'D', 8, 0},
		{datatype.FieldTypeUnknown, 'C', 254, 0},
	}

	for _, tt := range tests {
		t.Run(string(tt.commonType), func(t *testing.T) {
			gotTypeString, gotSizeInt, gotPrecInt := mapper.FromCommon(tt.commonType)
			gotType := byte(0)
			if gotTypeString != "" {
				gotType = gotTypeString[0]
			}
			gotSize := uint8(gotSizeInt)
			gotPrec := uint8(gotPrecInt)
			if gotType != tt.wantType {
				t.Errorf("CommonToShapefileDBF(%v) type = %c, want %c", tt.commonType, gotType, tt.wantType)
			}
			if gotSize != tt.wantSize {
				t.Errorf("CommonToShapefileDBF(%v) size = %d, want %d", tt.commonType, gotSize, tt.wantSize)
			}
			if gotPrec != tt.wantPrec {
				t.Errorf("CommonToShapefileDBF(%v) precision = %d, want %d", tt.commonType, gotPrec, tt.wantPrec)
			}
		})
	}
}
