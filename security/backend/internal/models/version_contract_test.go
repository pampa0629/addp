package models

import (
	"encoding/json"
	"testing"
)

func TestSecurityResourceVersionsAreJSONIntegers(t *testing.T) {
	for _, test := range []struct {
		name     string
		resource any
	}{
		{"classification", SecurityClassification{Version: 3}},
		{"grade", SecurityGrade{Version: 3}},
		{"sensitive data type", SensitiveDataType{Version: 3}},
		{"detector", Detector{Version: 3}},
		{"baseline", ProtectionBaseline{Version: 3}},
		{"finding baseline", FindingProtectionBaseline{Version: 3}},
		{"enrollment", ProtectionEnrollment{Version: 3}},
		{"enrollment response", ProtectionEnrollmentResponse{Version: 3}},
		{"assessment", ResourceSecurityAssessmentResponse{ResourceSecurityAssessment: ResourceSecurityAssessment{Version: 3}}},
		{"policy", ProtectionPolicyResponse{ProtectionPolicy: ProtectionPolicy{Version: 3}}},
		{"access request", ProtectionAccessRequest{Version: 3}},
		{"exemption", ProtectionExemptionResponse{ProtectionExemption: ProtectionExemption{Version: 3}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, err := json.Marshal(test.resource)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(body, &fields); err != nil {
				t.Fatal(err)
			}
			if string(fields["version"]) != "3" {
				t.Fatalf("version must be a JSON integer, got %s", fields["version"])
			}
		})
	}
}
