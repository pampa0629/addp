package service

import (
	"strings"
	"testing"

	"github.com/addp/graph/internal/models"
	"gorm.io/datatypes"
)

func TestOntologySemanticsResolvesEntityTypesWithoutRepositoryAccess(t *testing.T) {
	ontology := &models.Ontology{EntityTypes: []models.EntityType{
		{
			ID:              1,
			Name:            "Person",
			Label:           "员工",
			NodeLabels:      datatypes.JSON(`["Person"]`),
			DisplayProperty: "note",
			Properties: datatypes.JSON(`[
				{"name":"name","data_type":"string","searchable":true},
				{"name":"age","data_type":"integer","searchable":true},
				{"name":"note","data_type":"string","searchable":false}
			]`),
		},
	}}

	semantics := newOntologySemantics(ontology)
	name, label := semantics.resolveEntityType([]string{"Person"})
	if name != "Person" || label != "员工" {
		t.Fatalf("resolved entity type = %q/%q", name, label)
	}
	if got := semantics.searchableProperties(); len(got) != 2 || got[0] != "name" || got[1] != "note" {
		t.Fatalf("searchable properties = %#v, want [name note]", got)
	}
	if got := semantics.displayName([]string{"Person"}, map[string]interface{}{"note": "研发负责人"}, "person-1"); got != "研发负责人" {
		t.Fatalf("display name = %q", got)
	}
}

func TestBuildSearchIndexDefinitionsIncludesInheritedDisplayProperty(t *testing.T) {
	parentID := uint(1)
	ontology := &models.Ontology{EntityTypes: []models.EntityType{
		{
			ID:         parentID,
			Name:       "Person",
			Properties: datatypes.JSON(`[{"name":"full_name","data_type":"string","searchable":false}]`),
		},
		{
			ID:              2,
			Name:            "Employee",
			ParentID:        &parentID,
			DisplayProperty: "full_name",
			Properties:      datatypes.JSON(`[]`),
		},
	}}

	definitions := buildSearchIndexDefinitions(9, ontology)
	if len(definitions) != 1 {
		t.Fatalf("index definitions = %#v", definitions)
	}
	if got := definitions[0]; got.EntityType != "Employee" || len(got.Properties) != 1 || got.Properties[0] != "full_name" {
		t.Fatalf("employee index = %#v", got)
	}
}

func TestBuildSearchIndexDefinitionsKeepPropertiesScopedToEntityTypes(t *testing.T) {
	ontology := &models.Ontology{EntityTypes: []models.EntityType{
		{
			ID:         1,
			Name:       "Person",
			NodeLabels: datatypes.JSON(`["Person","Employee"]`),
			Properties: datatypes.JSON(`[{"name":"name","data_type":"string","searchable":true}]`),
		},
		{
			ID:         2,
			Name:       "Company",
			NodeLabels: datatypes.JSON(`["Company"]`),
			Properties: datatypes.JSON(`[{"name":"title","data_type":"string","searchable":true}]`),
		},
	}}

	definitions := buildSearchIndexDefinitions(7, ontology)
	if len(definitions) != 2 {
		t.Fatalf("index definitions = %#v, want two entity-scoped indexes", definitions)
	}
	if got := definitions[0]; got.EntityType != "Company" || len(got.Labels) != 1 || got.Labels[0] != "Company" || len(got.Properties) != 1 || got.Properties[0] != "title" {
		t.Fatalf("company index = %#v", got)
	}
	if got := definitions[1]; got.EntityType != "Person" || len(got.Labels) != 2 || got.Labels[0] != "Employee" || got.Labels[1] != "Person" || len(got.Properties) != 1 || got.Properties[0] != "name" {
		t.Fatalf("person index = %#v", got)
	}
}

func TestFulltextSearchSubqueryUsesEntityScopedIndexesAndLabelSets(t *testing.T) {
	definitions := []searchIndexDefinition{
		{Name: "person-index", EntityType: "Person", Labels: []string{"Employee", "Person"}, Properties: []string{"name"}},
		{Name: "company-index", EntityType: "Company", Labels: []string{"Company"}, Properties: []string{"title"}},
	}

	query := fulltextSearchSubquery(definitions, `Ada "Lovelace"`)
	for _, fragment := range []string{
		"db.index.fulltext.queryNodes('person-index'",
		"all(label IN ['Employee','Person'] WHERE label IN labels(node))",
		"db.index.fulltext.queryNodes('company-index'",
		"UNION ALL",
		`'"Ada \\"Lovelace\\""'`,
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("fulltextSearchSubquery() = %q, missing %q", query, fragment)
		}
	}
}

func TestRowsToNodeScoresUsesOntologyDisplayProperty(t *testing.T) {
	semantics := newOntologySemantics(&models.Ontology{EntityTypes: []models.EntityType{
		{Name: "Person", NodeLabels: datatypes.JSON(`["Person"]`), DisplayProperty: "employee_no"},
	}})
	rows := []map[string]interface{}{
		{
			"node_id":         "person-1",
			"node_labels":     []string{"Person"},
			"node_properties": map[string]interface{}{"name": "Ada", "employee_no": "E-001"},
			"score":           12.0,
		},
		{
			"node_id":         "company-1",
			"node_labels":     []string{"Company"},
			"node_properties": map[string]interface{}{"name": "ADDP"},
			"score":           5.0,
		},
	}

	scores := rowsToNodeScores(rows, semantics)
	if scores[0].DisplayName != "E-001" {
		t.Fatalf("configured display name = %q", scores[0].DisplayName)
	}
	if scores[1].DisplayName != "company-1" {
		t.Fatalf("fallback display name = %q", scores[1].DisplayName)
	}
}
