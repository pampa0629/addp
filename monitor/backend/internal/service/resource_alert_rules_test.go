package service

import (
	"encoding/json"
	"testing"
)

func TestResourceRulePolicyRequiresExplicitRecoverableValues(t *testing.T) {
	for _, body := range []string{`{}`, `{"trigger_percent":80,"trigger_seconds":30,"recovery_seconds":30}`, `{"trigger_percent":80,"recovery_percent":null,"trigger_seconds":30,"recovery_seconds":30}`, `{"trigger_percent":80,"recovery_percent":0,"trigger_seconds":null,"recovery_seconds":30}`, `{"trigger_percent":80,"recovery_percent":0,"trigger_seconds":30,"recovery_seconds":30}`} {
		var input ResourceAlertRuleInput
		if e := json.Unmarshal([]byte(body), &input); e != nil {
			t.Fatal(e)
		}
		if _, ok := input.pressurePolicy(); ok {
			t.Fatal("missing/null policy accepted", body)
		}
	}
	var input ResourceAlertRuleInput
	if e := json.Unmarshal([]byte(`{"trigger_percent":80,"recovery_percent":25,"trigger_seconds":30,"recovery_seconds":30}`), &input); e != nil {
		t.Fatal(e)
	}
	policy, ok := input.pressurePolicy()
	if !ok || policy.RecoveryPercent != 25 {
		t.Fatal("explicit valid threshold rejected", policy)
	}
}
