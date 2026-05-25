package preview

import (
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
)

func TestGraphOverviewRowsUsesGraphInfo(t *testing.T) {
	t.Parallel()

	nodeCount := int64(3)
	relCount := int64(2)
	info := &datatype.GraphInfo{
		NodeShapes: []datatype.GraphNodeShapeInfo{{
			Name:       "Person",
			Properties: []datatype.FieldInfo{{Name: "name"}},
			Count:      &nodeCount,
		}},
		RelationshipShapes: []datatype.GraphRelationshipShapeInfo{{
			Type: "WORKS_FOR",
			Patterns: []datatype.GraphRelationshipPatternInfo{{
				From: datatype.GraphEndpointInfo{ShapeName: "Person"},
				To:   datatype.GraphEndpointInfo{ShapeName: "Company"},
			}},
			Count: &relCount,
		}},
	}

	columns, rows := graphOverviewRows(info)
	if !reflect.DeepEqual(columns, []string{"kind", "name", "count", "patterns", "properties"}) {
		t.Fatalf("columns = %v", columns)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %v", rows)
	}
	if rows[0]["kind"] != "node_shape" || rows[0]["name"] != "Person" || rows[0]["properties"] != "name" {
		t.Fatalf("node row = %#v", rows[0])
	}
	if rows[1]["kind"] != "relationship_shape" || rows[1]["patterns"] != "(Person)->(Company)" {
		t.Fatalf("relationship row = %#v", rows[1])
	}
}

func TestFlattenGraphEntityRowsIncludesEntityFields(t *testing.T) {
	t.Parallel()

	source := []map[string]interface{}{
		{
			"r": map[string]interface{}{
				"id":         "rel-1",
				"type":       "WORKS_AT",
				"properties": map[string]interface{}{},
			},
		},
	}

	columns, rows := flattenGraphEntityRows(source, "r")
	wantColumns := []string{"id", "type"}
	if !reflect.DeepEqual(columns, wantColumns) {
		t.Fatalf("columns = %v, want %v", columns, wantColumns)
	}
	if len(rows) != 1 || rows[0]["id"] != "rel-1" || rows[0]["type"] != "WORKS_AT" {
		t.Fatalf("rows = %v, want relationship identity fields", rows)
	}
}
