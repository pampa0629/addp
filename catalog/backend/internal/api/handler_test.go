package api

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/catalog/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestMapUpdateEntryRequestParsesCanonicalCrossModuleIDs(t *testing.T) {
	componentID := uuid.New()
	input, err := mapUpdateEntryRequest(updateEntryRequest{
		Version: 3, GovernanceStatus: models.GovernanceStatusCurated, Visibility: models.VisibilityTenant,
		Domains:     []updateDomainLinkRequest{{ID: "9223372036854775807", Role: models.SemanticRolePrimary}},
		GlossaryIDs: []string{"20"},
		Responsibilities: []updateResponsibilityRequest{{
			Role: models.ResponsibilityRoleBusinessOwner, SubjectType: models.ResponsibilitySubjectUser,
			SubjectID: "9007199254740993",
		}},
		ComponentElements: []updateComponentElementRequest{{ComponentID: componentID.String(), ElementID: "50"}},
	})
	if err != nil {
		t.Fatalf("mapUpdateEntryRequest() error = %v", err)
	}
	if input.Domains[0].ID != int64(9223372036854775807) || input.Responsibilities[0].SubjectID != int64(9007199254740993) || input.ComponentElements[0].ComponentID != componentID {
		t.Fatalf("input = %#v", input)
	}
}

func TestMapUpdateEntryGovernanceRequestRejectsNonCanonicalSuccessorUUID(t *testing.T) {
	for _, id := range []string{"", "not-a-uuid", " 00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000000"} {
		_, err := mapUpdateEntryGovernanceRequest(updateEntryGovernanceRequest{
			Version: 1, GovernanceStatus: models.GovernanceStatusDeprecated,
			RecommendedSuccessorEntryID: stringPointer(id),
		})
		if err == nil {
			t.Fatalf("successor UUID %q accepted", id)
		}
	}
}

func TestMapUpdateEntryGovernanceRequestPreservesLifecycleFields(t *testing.T) {
	successorID := uuid.New()
	reason := "Superseded by the canonical resource"
	input, err := mapUpdateEntryGovernanceRequest(updateEntryGovernanceRequest{
		Version: 4, GovernanceStatus: models.GovernanceStatusDeprecated,
		Reason: &reason, RecommendedSuccessorEntryID: stringPointer(successorID.String()),
	})
	if err != nil {
		t.Fatalf("mapUpdateEntryGovernanceRequest() error = %v", err)
	}
	if input.Version != 4 || input.GovernanceStatus != models.GovernanceStatusDeprecated ||
		input.Reason == nil || *input.Reason != reason || input.RecommendedSuccessorEntryID == nil ||
		*input.RecommendedSuccessorEntryID != successorID {
		t.Fatalf("input = %#v", input)
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

func TestMapBatchGovernanceRequestParsesExplicitVersionedMembers(t *testing.T) {
	first := uuid.New()
	second := uuid.New()
	input, err := mapBatchGovernanceRequest(batchGovernanceRequest{
		Entries:   []batchGovernanceEntryRequest{{ID: first.String(), Version: 2}, {ID: second.String(), Version: 5}},
		Operation: service.BatchGovernanceAssignAccountableDepartment, ReferenceID: "9007199254740993",
	})
	if err != nil {
		t.Fatalf("mapBatchGovernanceRequest() error = %v", err)
	}
	if input.ReferenceID != 9007199254740993 || len(input.Entries) != 2 || input.Entries[0].ID != first || input.Entries[1].Version != 5 {
		t.Fatalf("input = %#v", input)
	}
}

func TestMapBatchGovernanceRequestRejectsImplicitOrNonCanonicalMembers(t *testing.T) {
	id := uuid.New().String()
	requests := []batchGovernanceRequest{
		{Operation: service.BatchGovernanceAssignPrimaryDomain, ReferenceID: "1"},
		{Entries: []batchGovernanceEntryRequest{{ID: id, Version: 1}, {ID: id, Version: 1}}, Operation: service.BatchGovernanceAssignPrimaryDomain, ReferenceID: "1"},
		{Entries: []batchGovernanceEntryRequest{{ID: strings.ToUpper(id), Version: 1}}, Operation: service.BatchGovernanceAssignPrimaryDomain, ReferenceID: "1"},
		{Entries: []batchGovernanceEntryRequest{{ID: id, Version: 0}}, Operation: service.BatchGovernanceAssignPrimaryDomain, ReferenceID: "1"},
		{Entries: []batchGovernanceEntryRequest{{ID: id, Version: 1}}, Operation: service.BatchGovernanceAssignPrimaryDomain, ReferenceID: "01"},
	}
	for index, request := range requests {
		if _, err := mapBatchGovernanceRequest(request); err == nil {
			t.Fatalf("request %d accepted: %#v", index, request)
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

func TestCatalogCollectionProjectGroupOptionIDMarshalsAsString(t *testing.T) {
	payload, err := json.Marshal(service.CollectionProjectGroupOption{ProjectGroupID: 9007199254740993, Name: "Delivery"})
	if err != nil {
		t.Fatal(err)
	}
	if text := string(payload); !contains(text, `"project_group_id":"9007199254740993"`) {
		t.Fatalf("payload %s does not preserve project_group_id exactly", text)
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

func TestRespondErrorMapsDataDictionaryContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		err       error
		status    int
		errorCode string
	}{
		{err: service.ErrDataDictionaryNotApplicable, status: http.StatusConflict, errorCode: "catalog_data_dictionary_not_applicable"},
		{err: service.ErrDataDictionaryDependencyUnavailable, status: http.StatusServiceUnavailable, errorCode: "catalog_data_dictionary_dependency_unavailable"},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		respondError(context, http.StatusInternalServerError, test.err)
		if recorder.Code != test.status {
			t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.status, recorder.Body.String())
		}
		var response map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response["error_code"] != test.errorCode {
			t.Fatalf("error_code = %#v, want %s", response["error_code"], test.errorCode)
		}
	}
}

func TestRespondErrorMapsGovernanceContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		err       error
		status    int
		errorCode string
	}{
		{err: service.ErrCertificationRequirementsNotMet, status: http.StatusBadRequest, errorCode: "catalog_certification_requirements_not_met"},
		{err: service.ErrCertificationWithdrawalReasonRequired, status: http.StatusBadRequest, errorCode: "catalog_certification_withdrawal_reason_required"},
		{err: service.ErrInvalidGovernanceUpdate, status: http.StatusBadRequest, errorCode: "invalid_request"},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		respondError(context, http.StatusInternalServerError, test.err)
		if recorder.Code != test.status {
			t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.status, recorder.Body.String())
		}
		var response map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response["error_code"] != test.errorCode {
			t.Fatalf("error_code = %#v, want %s", response["error_code"], test.errorCode)
		}
	}
}

func TestMarshalDataDictionaryExportProducesVerifiableImmutableAttachment(t *testing.T) {
	entryID := uuid.MustParse("3e4df990-91c8-4fe5-b0bb-3d9c94085691")
	generatedAt := time.Date(2026, 8, 28, 12, 34, 56, 0, time.UTC)
	dictionary := &service.DataDictionary{
		SchemaVersion: service.DataDictionarySchemaVersion,
		EntryID:       entryID,
		AsOf:          time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		GeneratedAt:   generatedAt,
		Fields:        []service.DataDictionaryField{},
	}
	payload, etag, fileName, err := marshalDataDictionaryExport(dictionary)
	if err != nil {
		t.Fatalf("marshalDataDictionaryExport() error = %v", err)
	}
	if !json.Valid(payload) || !strings.HasSuffix(string(payload), "\n") {
		t.Fatalf("payload is not newline-terminated JSON: %q", payload)
	}
	digest := sha256.Sum256(payload)
	if etag != fmt.Sprintf(`"%x"`, digest) {
		t.Fatalf("etag = %q", etag)
	}
	wantFileName := "data-dictionary-3e4df990-91c8-4fe5-b0bb-3d9c94085691-20260828T123456Z.json"
	if fileName != wantFileName {
		t.Fatalf("fileName = %q, want %q", fileName, wantFileName)
	}
	secondPayload, secondETag, secondFileName, err := marshalDataDictionaryExport(dictionary)
	if err != nil || string(secondPayload) != string(payload) || secondETag != etag || secondFileName != fileName {
		t.Fatalf("same snapshot produced different attachment: err=%v etag=%q file=%q", err, secondETag, secondFileName)
	}
}

func TestMarshalDataDictionaryExportRejectsMissingSnapshotIdentity(t *testing.T) {
	if _, _, _, err := marshalDataDictionaryExport(&service.DataDictionary{}); err == nil {
		t.Fatal("empty data dictionary export was accepted")
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
