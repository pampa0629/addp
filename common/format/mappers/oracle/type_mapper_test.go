package oracle

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestTypeMapperToCommon(t *testing.T) {
	mapper := &TypeMapper{}
	tests := []struct {
		native string
		want   datatype.FieldType
	}{
		{native: "NUMBER(9,0)", want: datatype.FieldTypeInt},
		{native: "NUMBER(18,0)", want: datatype.FieldTypeBigInt},
		{native: "NUMBER(38,0)", want: datatype.FieldTypeDecimal},
		{native: "NUMBER(12,2)", want: datatype.FieldTypeDecimal},
		{native: "NUMBER", want: datatype.FieldTypeDecimal},
		{native: "VARCHAR2(255)", want: datatype.FieldTypeString},
		{native: "TIMESTAMP(6) WITH TIME ZONE", want: datatype.FieldTypeTimestamp},
		{native: "DATE", want: datatype.FieldTypeTimestamp},
		{native: "BINARY_FLOAT", want: datatype.FieldTypeFloat},
		{native: "BLOB", want: datatype.FieldTypeBytes},
		{native: "JSON", want: datatype.FieldTypeJSON},
		{native: "MDSYS.SDO_GEOMETRY", want: datatype.FieldTypeGeometry},
		{native: "XMLTYPE", want: datatype.FieldTypeUnknown},
		{native: "INTERVAL DAY TO SECOND", want: datatype.FieldTypeUnknown},
	}
	for _, test := range tests {
		t.Run(test.native, func(t *testing.T) {
			if got := mapper.ToCommon(test.native); got != test.want {
				t.Fatalf("ToCommon(%q) = %q, want %q", test.native, got, test.want)
			}
		})
	}
}
