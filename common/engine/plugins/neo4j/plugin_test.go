package neo4j

import (
	"reflect"
	"testing"
)

func TestGraphEndpointShapeNameUsesStableLabelSetName(t *testing.T) {
	labels := []string{"Employee", "Person"}

	got := graphEndpointShapeName(labels)
	if got != "Employee+Person" {
		t.Fatalf("graphEndpointShapeName() = %q, want %q", got, "Employee+Person")
	}

	if !reflect.DeepEqual(labels, []string{"Employee", "Person"}) {
		t.Fatalf("graphEndpointShapeName mutated input labels: %#v", labels)
	}
}

func TestEscapeCypherLabelsEscapesEachLabel(t *testing.T) {
	got := escapeCypherLabels([]string{"Person", "We`ird"})
	want := []string{"`Person`", "`We``ird`"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("escapeCypherLabels() = %#v, want %#v", got, want)
	}
}

func TestSampleGraphQueryFiltersByNodeShape(t *testing.T) {
	got := sampleGraphQuery(map[string]interface{}{
		"kind":   "node_shape",
		"labels": []string{"Employee", "Person"},
	}, 10)
	want := "MATCH (n:`Employee`:`Person`) RETURN n LIMIT 10"
	if got != want {
		t.Fatalf("sampleGraphQuery() = %q, want %q", got, want)
	}
}

func TestSampleGraphQueryFiltersByRelationshipShape(t *testing.T) {
	got := sampleGraphQuery(map[string]interface{}{
		"kind":        "relationship_shape",
		"type":        "WORKS_AT",
		"from_labels": []string{"Person"},
		"to_labels":   []string{"Company"},
	}, 10)
	want := "MATCH (n:`Person`)-[r:`WORKS_AT`]->(m:`Company`) RETURN n, r, m LIMIT 10"
	if got != want {
		t.Fatalf("sampleGraphQuery() = %q, want %q", got, want)
	}
}
