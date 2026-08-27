package neo4j

import (
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestNeo4jDatabaseFromCatalogPath(t *testing.T) {
	database, ok := neo4jDatabaseFromCatalogPath(plugin.EngineCatalogPath{
		Version:  plugin.EngineCatalogPathVersion,
		EngineID: 9,
		Segments: []plugin.EngineCatalogSegment{
			{Term: plugin.EngineCatalogTermServer, Kind: plugin.EngineCatalogTermServer},
			{Term: plugin.EngineCatalogTermDatabase, Kind: plugin.EngineCatalogKindNamespace, Name: "business"},
			{Term: plugin.EngineCatalogTermGraph, Kind: plugin.EngineCatalogKindGraph, Name: plugin.EngineCatalogKindGraph},
		},
	})
	if !ok || database != "business" {
		t.Fatalf("neo4jDatabaseFromCatalogPath() = (%q, %t)", database, ok)
	}
}

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
	got := sampleGraphQuery(plugin.GraphSampleFilter{
		Kind:   plugin.GraphSampleKindNodeShape,
		Labels: []string{"Employee", "Person"},
	}, 10)
	want := "MATCH (n:`Employee`:`Person`) RETURN n LIMIT 10"
	if got != want {
		t.Fatalf("sampleGraphQuery() = %q, want %q", got, want)
	}
}

func TestSampleGraphQueryFiltersByRelationshipShape(t *testing.T) {
	got := sampleGraphQuery(plugin.GraphSampleFilter{
		Kind:             plugin.GraphSampleKindRelationshipShape,
		RelationshipType: "WORKS_AT",
		FromLabels:       []string{"Person"},
		ToLabels:         []string{"Company"},
	}, 10)
	want := "MATCH (n:`Person`)-[r:`WORKS_AT`]->(m:`Company`) RETURN n, r, m LIMIT 10"
	if got != want {
		t.Fatalf("sampleGraphQuery() = %q, want %q", got, want)
	}
}

func TestSampleGraphQuerySkipsInternalNodeShape(t *testing.T) {
	got := sampleGraphQuery(plugin.GraphSampleFilter{
		Kind:   plugin.GraphSampleKindNodeShape,
		Labels: []string{"SpatialLayer"},
	}, 10)
	want := "MATCH (n) WHERE false RETURN n LIMIT 10"
	if got != want {
		t.Fatalf("sampleGraphQuery() = %q, want %q", got, want)
	}
}

func TestInternalNodeLabelSetDetectsSpatialLayer(t *testing.T) {
	if !isInternalNodeLabelSet([]string{"SpatialLayer"}) {
		t.Fatal("isInternalNodeLabelSet() = false, want true")
	}
	if isInternalNodeLabelSet([]string{"Person"}) {
		t.Fatal("isInternalNodeLabelSet() = true, want false")
	}
}

func TestGraphNodeShapeKindDistinguishesSingleLabel(t *testing.T) {
	if got := graphNodeShapeKind([]string{"Person"}); got != datatype.GraphNodeShapeKindLabel {
		t.Fatalf("graphNodeShapeKind(single) = %q, want %q", got, datatype.GraphNodeShapeKindLabel)
	}
	if got := graphNodeShapeKind([]string{"Employee", "Person"}); got != datatype.GraphNodeShapeKindLabelSet {
		t.Fatalf("graphNodeShapeKind(label set) = %q, want %q", got, datatype.GraphNodeShapeKindLabelSet)
	}
}

func TestBoundedCypherQueryAppliesOuterLimit(t *testing.T) {
	if got := boundedCypherQuery(" MATCH (n) RETURN n; ", 25); got != "CALL { MATCH (n) RETURN n } RETURN * LIMIT 25" {
		t.Fatalf("boundedCypherQuery() = %q", got)
	}
	if got := boundedCypherQuery("MATCH (n) RETURN n;", 0); got != "MATCH (n) RETURN n" {
		t.Fatalf("boundedCypherQuery(unbounded) = %q", got)
	}
}

func TestNeo4jQueryRejectsMissingParameterBeforeConnecting(t *testing.T) {
	provider := &Neo4jPlugin{}
	_, err := provider.executeQuery(t.Context(), plugin.ConnectionInfo{}, `MATCH (n) WHERE n.name = $name RETURN n`, plugin.QueryOptions{})
	if err == nil || !strings.Contains(err.Error(), `query parameter "name" is not provided`) {
		t.Fatalf("executeQuery() error = %v, want missing parameter error", err)
	}
}
