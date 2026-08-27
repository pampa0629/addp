package api

import (
	"encoding/json"
	"testing"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/catalog/internal/service"
	"github.com/google/uuid"
)

func TestMapUpdateEntryRequestParsesCanonicalCrossModuleIDs(t *testing.T) {
	componentID := uuid.New()
	successorID := uuid.New()
	input, err := mapUpdateEntryRequest(updateEntryRequest{
		Version: 3, GovernanceStatus: models.GovernanceStatusCurated, Visibility: models.VisibilityTenant,
		Domains:     []updateDomainLinkRequest{{ID: "9223372036854775807", Role: models.SemanticRolePrimary}},
		GlossaryIDs: []string{"20"},
		Responsibilities: []updateResponsibilityRequest{{
			Role: models.ResponsibilityRoleBusinessOwner, SubjectType: models.ResponsibilitySubjectUser,
			SubjectID: "9007199254740993",
		}},
		ComponentElements:           []updateComponentElementRequest{{ComponentID: componentID.String(), ElementID: "50"}},
		RecommendedSuccessorEntryID: stringPointer(successorID.String()),
	})
	if err != nil {
		t.Fatalf("mapUpdateEntryRequest() error = %v", err)
	}
	if input.Domains[0].ID != int64(9223372036854775807) || input.Responsibilities[0].SubjectID != int64(9007199254740993) || input.ComponentElements[0].ComponentID != componentID || input.RecommendedSuccessorEntryID == nil || *input.RecommendedSuccessorEntryID != successorID {
		t.Fatalf("input = %#v", input)
	}
}

func TestMapUpdateEntryRequestRejectsNonCanonicalSuccessorUUID(t *testing.T) {
	for _, id := range []string{"", "not-a-uuid", " 00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000000"} {
		_, err := mapUpdateEntryRequest(updateEntryRequest{
			Version: 1, GovernanceStatus: models.GovernanceStatusDeprecated, Visibility: models.VisibilityTenant,
			RecommendedSuccessorEntryID: stringPointer(id),
		})
		if err == nil {
			t.Fatalf("successor UUID %q accepted", id)
		}
	}
}

func TestMapUpdateEntryRequestRejectsNonCanonicalIDs(t *testing.T) {
	for _, id := range []string{"", "0", "01", "+1", "-1", "9223372036854775808"} {
		_, err := mapUpdateEntryRequest(updateEntryRequest{
			Version: 1, GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory,
			GlossaryIDs: []string{id},
		})
		if err == nil {
			t.Fatalf("id %q accepted", id)
		}
	}
}

func TestCatalogCrossModuleIDsMarshalAsStrings(t *testing.T) {
	payload, err := json.Marshal(struct {
		Responsibility models.Responsibility              `json:"responsibility"`
		Semantic       models.SemanticAssociation         `json:"semantic"`
		Element        models.ComponentElementAssociation `json:"element"`
	}{
		Responsibility: models.Responsibility{SubjectID: 9007199254740993},
		Semantic:       models.SemanticAssociation{SemanticID: 9007199254740994},
		Element:        models.ComponentElementAssociation{ElementID: 9007199254740995},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, expected := range []string{`"subject_id":"9007199254740993"`, `"semantic_id":"9007199254740994"`, `"element_id":"9007199254740995"`} {
		if !contains(text, expected) {
			t.Fatalf("payload %s missing %s", text, expected)
		}
	}
}

func TestCatalogCollectionSystemIDsMarshalAsStrings(t *testing.T) {
	payload, err := json.Marshal(models.Collection{ProjectGroupID: 9007199254740993, CreatedBy: 9007199254740994})
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, expected := range []string{`"project_group_id":"9007199254740993"`, `"created_by":"9007199254740994"`} {
		if !contains(text, expected) {
			t.Fatalf("payload %s missing %s", text, expected)
		}
	}
}

func TestCatalogEntrySummaryEngineIDMarshalsAsString(t *testing.T) {
	payload, err := json.Marshal(service.EntrySummary{SourceEngineID: 9007199254740993})
	if err != nil {
		t.Fatal(err)
	}
	if text := string(payload); !contains(text, `"source_engine_id":"9007199254740993"`) {
		t.Fatalf("payload %s does not preserve source_engine_id exactly", text)
	}
}

func contains(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}

func stringPointer(value string) *string { return &value }
