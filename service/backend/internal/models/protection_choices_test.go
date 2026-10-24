package models

import (
	"github.com/addp/common/dataprotection"
	"testing"
)

func TestQueryServiceProtectionChoicesRejectMalformedSavedConfiguration(t *testing.T) {
	choice := dataprotection.FieldRoleChoice{Target: dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item"}, ComponentKey: "phone", Selection: dataprotection.RoleOptionSelection{PolicyID: "policy", PolicyRevision: 1, AssessmentRevision: 1, OptionID: "option"}}
	for name, raw := range map[string]any{"null": nil, "unknown": []any{map[string]any{"algorithm": "allow"}}, "duplicate": []dataprotection.FieldRoleChoice{choice, choice}, "roles": []any{map[string]any{"role_keys": []string{"admin"}}}} {
		t.Run(name, func(t *testing.T) {
			if _, err := (&QueryService{DataConfig: JSONB{QueryProtectionChoicesKey: raw}}).ProtectionChoices(); err == nil {
				t.Fatal("invalid saved choice was ignored")
			}
		})
	}
	for _, q := range []*QueryService{nil, {}, {DataConfig: JSONB{QueryProtectionChoicesKey: []any{}}}} {
		if choices, err := q.ProtectionChoices(); err != nil || len(choices) != 0 {
			t.Fatal(choices, err)
		}
	}
	if choices, err := (&QueryService{DataConfig: JSONB{QueryProtectionChoicesKey: []dataprotection.FieldRoleChoice{choice}}}).ProtectionChoices(); err != nil || len(choices) != 1 || choices[0] != choice {
		t.Fatal(choices, err)
	}
}
