package plugin

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestCatalogFactsTableInfoReturnsClone(t *testing.T) {
	facts := &CatalogFacts{
		Table: &datatype.TableInfo{
			Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}},
		},
	}

	info := CatalogFactsTableInfo(facts)
	if info == nil || len(info.Fields) != 1 || info.Fields[0].Name != "id" {
		t.Fatalf("CatalogFactsTableInfo() = %#v", info)
	}
	info.Fields[0].Name = "changed"
	if facts.Table.Fields[0].Name != "id" {
		t.Fatalf("CatalogFactsTableInfo returned mutable table fields")
	}
}

func TestCatalogFactsGraphInfoReturnsClone(t *testing.T) {
	count := int64(3)
	facts := &CatalogFacts{
		Kind: "graph",
		Graph: &datatype.GraphInfo{
			NodeShapes: []datatype.GraphNodeShapeInfo{{
				Name:  "Person",
				Count: &count,
			}},
		},
	}

	info := CatalogFactsGraphInfo(facts)
	if info == nil || len(info.NodeShapes) != 1 || info.NodeShapes[0].Name != "Person" {
		t.Fatalf("CatalogFactsGraphInfo() = %#v", info)
	}
	info.NodeShapes[0].Name = "Changed"
	if facts.Graph.NodeShapes[0].Name != "Person" {
		t.Fatalf("CatalogFactsGraphInfo returned mutable graph info")
	}
}
