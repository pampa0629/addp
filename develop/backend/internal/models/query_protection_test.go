package models

import (
	"github.com/addp/common/dataprotection"
	"testing"
)

func TestQueryProtectionChoicesRejectsUntrustedRoleAndDecisionPayloads(t *testing.T) {
	base := map[string]interface{}{"target": map[string]interface{}{"owner_module": "meta", "resource_type": "data_item", "resource_identity": "fingerprint"}, "component_key": "phone", "selection": map[string]interface{}{"policy_id": "policy", "assessment_revision": 1, "policy_revision": 1, "option_id": "hash"}}
	for _, key := range []string{"role_keys", "decision", "roles", "expires_at"} {
		copy := map[string]interface{}{}
		for k, v := range base {
			copy[k] = v
		}
		copy[key] = "injected"
		if _, err := QueryProtectionChoices(map[string]interface{}{"query_type": "sql", "protection_choices": []any{copy}}); err == nil {
			t.Fatal("untrusted choice field accepted", key)
		}
	}
	if _, err := QueryProtectionChoices(map[string]interface{}{"protection_choices": nil}); err == nil {
		t.Fatal("null choices accepted")
	}
	if _, err := QueryProtectionChoices(map[string]interface{}{"query_type": "cypher", "protection_choices": []any{base}}); err == nil {
		t.Fatal("unsupported path accepted a choice")
	}
	if err := dataprotection.ValidateFieldRoleChoices([]dataprotection.FieldRoleChoice{{}, {}}); err == nil {
		t.Fatal("invalid choices accepted")
	}
}
