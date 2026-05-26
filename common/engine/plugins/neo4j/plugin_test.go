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
	want := []string{"Person", "We``ird"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("escapeCypherLabels() = %#v, want %#v", got, want)
	}
}
