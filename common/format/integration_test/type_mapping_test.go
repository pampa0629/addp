package integration_test

import (
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
		want   format.FieldType
	}{
		{"varchar", format.FieldTypeString},
		{"varchar(255)", format.FieldTypeString},
		{"text", format.FieldTypeString},
		{"character varying", format.FieldTypeString},
		{"smallint", format.FieldTypeInt},
		{"integer", format.FieldTypeInt},
		{"int", format.FieldTypeInt},
		{"bigint", format.FieldTypeBigInt},
		{"real", format.FieldTypeFloat},              // 4字节单精度
		{"double precision", format.FieldTypeDouble}, // 8字节双精度
		{"numeric", format.FieldTypeDecimal},
		{"numeric(10,2)", format.FieldTypeDecimal},
		{"boolean", format.FieldTypeBool},
		{"bool", format.FieldTypeBool},
		{"date", format.FieldTypeDate},
		{"time", format.FieldTypeTime},
		{"timestamp", format.FieldTypeTimestamp},
		{"timestamp with time zone", format.FieldTypeTimestamp},
		{"geometry", format.FieldTypeGeometry},
		{"point", format.FieldTypePoint},
		{"linestring", format.FieldTypeLineString},
		{"polygon", format.FieldTypePolygon},
		{"json", format.FieldTypeJSON},
		{"jsonb", format.FieldTypeJSON},
		{"uuid", format.FieldTypeUUID},
		{"integer[]", format.FieldTypeArray},
		{"text[]", format.FieldTypeArray},
		{"custom_type", format.FieldTypeUnknown},
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
		want      format.FieldType
	}{
		{"varchar", format.FieldTypeString},
		{"int", format.FieldTypeInt},
		{"bigint", format.FieldTypeBigInt},
		{"float", format.FieldTypeFloat},   // 4字节单精度
		{"double", format.FieldTypeDouble}, // 8字节双精度
		{"decimal", format.FieldTypeDecimal},
		{"datetime", format.FieldTypeTimestamp},
		{"json", format.FieldTypeJSON},
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
		want       format.FieldType
	}{
		{"text", format.FieldTypeString},
		{"varchar(255)", format.FieldTypeString},
		{"integer", format.FieldTypeInt},
		{"bigint", format.FieldTypeInt},
		{"real", format.FieldTypeDouble},
		{"boolean", format.FieldTypeBool},
		{"datetime", format.FieldTypeTimestamp},
		{"blob", format.FieldTypeBytes},
		{"geometry", format.FieldTypeGeometry},
		{"point", format.FieldTypePoint},
		{"linestring", format.FieldTypeLineString},
		{"polygon", format.FieldTypePolygon},
		{"multipoint", format.FieldTypeMultiPoint},
		{"custom_type", format.FieldTypeString},
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
		want    format.FieldType
	}{
		{'C', format.FieldTypeString},
		{'N', format.FieldTypeFloat},
		{'F', format.FieldTypeFloat},
		{'L', format.FieldTypeBool},
		{'D', format.FieldTypeDate},
		{'M', format.FieldTypeString},
		{'X', format.FieldTypeUnknown},
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
		commonType format.FieldType
		want       string
	}{
		{format.FieldTypeString, "TEXT"},
		{format.FieldTypeInt, "INTEGER"},
		{format.FieldTypeBigInt, "BIGINT"},
		{format.FieldTypeFloat, "REAL"},              // 4字节单精度
		{format.FieldTypeDouble, "DOUBLE PRECISION"}, // 8字节双精度
		{format.FieldTypeDecimal, "NUMERIC"},
		{format.FieldTypeBool, "BOOLEAN"},
		{format.FieldTypeDate, "DATE"},
		{format.FieldTypeTimestamp, "TIMESTAMP"},
		{format.FieldTypeGeometry, "GEOMETRY"},
		{format.FieldTypePoint, "GEOMETRY(Point)"},
		{format.FieldTypeJSON, "JSONB"},
		{format.FieldTypeUUID, "UUID"},
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
		commonType format.FieldType
		wantType   byte
		wantSize   uint8
		wantPrec   uint8
	}{
		{format.FieldTypeString, 'C', 254, 0},
		{format.FieldTypeInt, 'N', 18, 0},
		{format.FieldTypeBigInt, 'N', 18, 0},
		{format.FieldTypeFloat, 'F', 13, 6},   // 单精度
		{format.FieldTypeDouble, 'F', 20, 8},  // 双精度
		{format.FieldTypeDecimal, 'N', 20, 8}, // 高精度小数用 Numeric
		{format.FieldTypeBool, 'L', 1, 0},
		{format.FieldTypeDate, 'D', 8, 0},
		{format.FieldTypeUnknown, 'C', 254, 0},
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
