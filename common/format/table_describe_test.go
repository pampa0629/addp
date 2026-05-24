package format

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestTableDescribeConversionsKeepDatatypeFields(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 4},
		{Name: "name", Type: datatype.FieldTypeString, Size: 64},
	}
	schema := &datatype.TableInfo{
		Name:    "orders",
		Kind:    "table",
		Comment: "order facts",
		Fields:  fields,
	}

	result := TableDescribeResultFromSchema(schema)
	if len(result.Table.Fields) != 2 {
		t.Fatalf("TableDescribeResultFromSchema() fields len = %d, want 2", len(result.Table.Fields))
	}
	if result.Table.Fields[0].Precision != 18 || result.Table.Fields[0].Scale != 4 {
		t.Fatalf("datatype precision/scale = %d/%d, want 18/4", result.Table.Fields[0].Precision, result.Table.Fields[0].Scale)
	}
	if result.Table.Name != "orders" || result.Table.Kind != "table" || result.Table.Comment != "order facts" {
		t.Fatalf("datatype table facts = %#v", result.Table)
	}

	roundTrip := TableSchemaFromDescribeResult(result)
	if len(roundTrip.Fields) != 2 {
		t.Fatalf("TableSchemaFromDescribeResult() fields len = %d, want 2", len(roundTrip.Fields))
	}
	if roundTrip.Fields[0].Precision != 18 || roundTrip.Fields[0].Scale != 4 {
		t.Fatalf("round trip precision/scale = %d/%d, want 18/4", roundTrip.Fields[0].Precision, roundTrip.Fields[0].Scale)
	}
	if roundTrip.Name != "orders" || roundTrip.Kind != "table" || roundTrip.Comment != "order facts" {
		t.Fatalf("round trip table facts = %#v", roundTrip)
	}
}
