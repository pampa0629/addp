package service

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestDuckDBDescribeContractBuildsPublishedFields(t *testing.T) {
	t.Parallel()

	table, spatial, err := duckDBDescribeContract(&plugin.QueryResult{Rows: []map[string]interface{}{
		{"column_name": "id", "column_type": "BIGINT", "null": "NO"},
		{"column_name": "shape", "column_type": "GEOMETRY", "null": "YES"},
	}})
	if err != nil {
		t.Fatalf("duckDBDescribeContract() error = %v", err)
	}
	if len(table.Fields) != 2 || table.Fields[0].Type != datatype.FieldTypeBigInt || table.Fields[1].Type != datatype.FieldTypeGeometry {
		t.Fatalf("table fields = %#v", table.Fields)
	}
	if spatial == nil || spatial.PrimaryGeometryColumn != "shape" {
		t.Fatalf("spatial = %#v", spatial)
	}
}
