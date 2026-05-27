package service

import (
	"testing"

	"github.com/addp/graph/internal/models"
	"gorm.io/datatypes"
)

func TestEffectiveNodeLabelsUsesExplicitMapping(t *testing.T) {
	et := &models.EntityType{
		Name:       "Person",
		NodeLabels: datatypes.JSON(`[" Employee ", "Person", "Employee", ""]`),
	}

	labels := effectiveNodeLabels(et, nil)

	if len(labels) != 2 || labels[0] != "Employee" || labels[1] != "Person" {
		t.Fatalf("unexpected labels: %#v", labels)
	}
}

func TestEffectiveNodeLabelsFallsBackToInheritanceChain(t *testing.T) {
	parent := models.EntityType{ID: 1, Name: "POI"}
	parentID := uint(1)
	child := models.EntityType{ID: 2, Name: "City", ParentID: &parentID}
	byID := entityTypeByID([]models.EntityType{parent, child})

	labels := effectiveNodeLabels(&child, byID)

	if len(labels) != 2 || labels[0] != "City" || labels[1] != "POI" {
		t.Fatalf("unexpected labels: %#v", labels)
	}
}

func TestSameStringSetIgnoresOrderWhitespaceAndDuplicates(t *testing.T) {
	if !sameStringSet([]string{"Person", " Employee ", "Person"}, []string{"Employee", "Person"}) {
		t.Fatal("expected string sets to be equal")
	}
	if sameStringSet([]string{"Person"}, []string{"Person", "Organization"}) {
		t.Fatal("expected different string sets")
	}
}

func TestEntityTypeNodeLabelsFallsBackToShapeName(t *testing.T) {
	labels := entityTypeNodeLabels(nil, "City+POI")

	if len(labels) != 2 || labels[0] != "City" || labels[1] != "POI" {
		t.Fatalf("unexpected labels: %#v", labels)
	}
}

func TestBusinessGraphFilterDetectsInternalSpatialNodes(t *testing.T) {
	if !isInternalNodeLabelSet([]string{"SpatialLayer"}) {
		t.Fatal("expected SpatialLayer to be treated as an internal node label")
	}
	if !isInternalNodeLabelSet([]string{"Business", "SpatialLayer"}) {
		t.Fatal("expected label set containing SpatialLayer to be internal")
	}
	if isInternalNodeLabelSet([]string{"City", "POI"}) {
		t.Fatal("expected business label set to be visible")
	}
}

func TestBusinessRelationshipPredicateFiltersInternalEndpoints(t *testing.T) {
	got := businessRelationshipPredicate("r", "source", "target")
	want := "NOT (type(r) IN ['RTREE_METADATA', 'RTREE_REFERENCE', 'RTREE_ROOT']) AND NOT (any(label IN labels(source) WHERE label IN ['SpatialLayer'])) AND NOT (any(label IN labels(target) WHERE label IN ['SpatialLayer']))"
	if got != want {
		t.Fatalf("unexpected predicate:\nwant: %s\n got: %s", want, got)
	}
}
