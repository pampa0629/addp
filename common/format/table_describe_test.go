package format

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestFieldInfoConversionsPreservePrecisionAndScale(t *testing.T) {
	fields := []FieldInfo{
		{Name: "amount", Type: datatype.FieldTypeDecimal, Size: 18, Precision: 4},
		{Name: "name", Type: datatype.FieldTypeString, Size: 64},
	}

	datatypeFields := DatatypeFieldInfos(fields)
	if len(datatypeFields) != 2 {
		t.Fatalf("DatatypeFieldInfos() len = %d, want 2", len(datatypeFields))
	}
	if datatypeFields[0].Precision != 18 || datatypeFields[0].Scale != 4 {
		t.Fatalf("datatype precision/scale = %d/%d, want 18/4", datatypeFields[0].Precision, datatypeFields[0].Scale)
	}
	if datatypeFields[0].Size != 0 {
		t.Fatalf("datatype decimal size = %d, want 0", datatypeFields[0].Size)
	}
	if datatypeFields[1].Size != 64 || datatypeFields[1].Precision != 0 || datatypeFields[1].Scale != 0 {
		t.Fatalf("datatype string size/precision/scale = %d/%d/%d, want 64/0/0", datatypeFields[1].Size, datatypeFields[1].Precision, datatypeFields[1].Scale)
	}

	formatFields := FormatFieldInfos(datatypeFields)
	if len(formatFields) != 2 {
		t.Fatalf("FormatFieldInfos() len = %d, want 2", len(formatFields))
	}
	if formatFields[0].Size != 18 || formatFields[0].Precision != 4 {
		t.Fatalf("format size/precision = %d/%d, want 18/4", formatFields[0].Size, formatFields[0].Precision)
	}
	if formatFields[1].Size != 64 || formatFields[1].Precision != 0 {
		t.Fatalf("format string size/precision = %d/%d, want 64/0", formatFields[1].Size, formatFields[1].Precision)
	}
}

func TestFormatFieldInfosUsesDatatypePrecisionWhenSizeIsUnset(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 4},
	}

	formatFields := FormatFieldInfos(fields)
	if len(formatFields) != 1 {
		t.Fatalf("FormatFieldInfos() len = %d, want 1", len(formatFields))
	}
	if formatFields[0].Size != 18 || formatFields[0].Precision != 4 {
		t.Fatalf("format size/precision = %d/%d, want 18/4", formatFields[0].Size, formatFields[0].Precision)
	}
}
