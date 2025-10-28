package integration_test

import (
	"testing"

	"github.com/addp/common/format"

	// 导入内置类型映射器
	_ "github.com/addp/common/format/builtin"
)

func TestTypeMappingPostgreSQLToCommon(t *testing.T) {
	mapper := &format.TypeMapping{}

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
		{"real", format.FieldTypeFloat},
		{"double precision", format.FieldTypeFloat},
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
			got := mapper.PostgreSQLToCommon(tt.pgType)
			if got != tt.want {
				t.Errorf("PostgreSQLToCommon(%q) = %v, want %v", tt.pgType, got, tt.want)
			}
		})
	}
}

func TestTypeMappingMySQLToCommon(t *testing.T) {
	mapper := &format.TypeMapping{}

	tests := []struct {
		mysqlType string
		want      format.FieldType
	}{
		{"varchar", format.FieldTypeString},
		{"int", format.FieldTypeInt},
		{"bigint", format.FieldTypeBigInt},
		{"float", format.FieldTypeFloat},
		{"double", format.FieldTypeFloat},
		{"decimal", format.FieldTypeDecimal},
		{"datetime", format.FieldTypeTimestamp},
		{"json", format.FieldTypeJSON},
	}

	for _, tt := range tests {
		t.Run(tt.mysqlType, func(t *testing.T) {
			got := mapper.MySQLToCommon(tt.mysqlType)
			if got != tt.want {
				t.Errorf("MySQLToCommon(%q) = %v, want %v", tt.mysqlType, got, tt.want)
			}
		})
	}
}

func TestTypeMappingShapefileDBFToCommon(t *testing.T) {
	mapper := &format.TypeMapping{}

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
			got := mapper.ShapefileDBFToCommon(tt.dbfType)
			if got != tt.want {
				t.Errorf("ShapefileDBFToCommon(%q) = %v, want %v", tt.dbfType, got, tt.want)
			}
		})
	}
}

func TestTypeMappingCommonToPostgreSQL(t *testing.T) {
	mapper := &format.TypeMapping{}

	tests := []struct {
		commonType format.FieldType
		want       string
	}{
		{format.FieldTypeString, "TEXT"},
		{format.FieldTypeInt, "INTEGER"},
		{format.FieldTypeBigInt, "BIGINT"},
		{format.FieldTypeFloat, "DOUBLE PRECISION"},
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
			got := mapper.CommonToPostgreSQL(tt.commonType)
			if got != tt.want {
				t.Errorf("CommonToPostgreSQL(%v) = %q, want %q", tt.commonType, got, tt.want)
			}
		})
	}
}

func TestTypeMappingCommonToShapefileDBF(t *testing.T) {
	mapper := &format.TypeMapping{}

	tests := []struct {
		commonType format.FieldType
		wantType   byte
		wantSize   uint8
		wantPrec   uint8
	}{
		{format.FieldTypeString, 'C', 254, 0},
		{format.FieldTypeInt, 'N', 18, 0},
		{format.FieldTypeBigInt, 'N', 18, 0},
		{format.FieldTypeFloat, 'F', 20, 8},
		{format.FieldTypeDecimal, 'F', 20, 8},
		{format.FieldTypeBool, 'L', 1, 0},
		{format.FieldTypeDate, 'D', 8, 0},
		{format.FieldTypeUnknown, 'C', 254, 0},
	}

	for _, tt := range tests {
		t.Run(string(tt.commonType), func(t *testing.T) {
			gotType, gotSize, gotPrec := mapper.CommonToShapefileDBF(tt.commonType)
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
