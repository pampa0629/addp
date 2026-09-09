package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProtectionAccessRequestResponseUsesActorObjectsOnly(t *testing.T) {
	reviewer := ProtectionAccessActor{Type: "user", ID: "32", DisplayName: "Reviewer"}
	response := ProtectionAccessRequestResponse{
		ProtectionAccessRequest: ProtectionAccessRequest{
			SubjectType: "user",
			SubjectID:   "4",
			DecidedBy:   func() *int64 { value := int64(32); return &value }(),
		},
		Requester:    ProtectionAccessActor{Type: "user", ID: "4", DisplayName: "Requester"},
		Reviewer:     &reviewer,
		EnrollmentID: "5cba79c0-5900-4250-9768-89af983d89cf",
	}
	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(payload)
	for _, expected := range []string{`"requester":{"type":"user","id":"4","display_name":"Requester"}`, `"reviewer":{"type":"user","id":"32","display_name":"Reviewer"}`, `"enrollment_id":"5cba79c0-5900-4250-9768-89af983d89cf"`} {
		if !strings.Contains(jsonText, expected) {
			t.Fatalf("response JSON %s missing %s", jsonText, expected)
		}
	}
	for _, forbidden := range []string{`"subject_type"`, `"subject_id"`, `"decided_by"`} {
		if strings.Contains(jsonText, forbidden) {
			t.Fatalf("response JSON %s contains legacy actor field %s", jsonText, forbidden)
		}
	}
}
