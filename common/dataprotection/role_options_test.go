package dataprotection

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestRoleOptionsAreExplicitUnionAndNeverPlaintext(t *testing.T) {
	now := time.Now().UTC()
	rule := testProjection(now).Rules[0]
	rule.RoleOptions = &RoleMaskingOptions{
		PolicyID: "11111111-1111-4111-8111-111111111111", AssessmentRevision: 1, PolicyRevision: 2,
		Options: []RoleMaskingOption{{ID: "22222222-2222-4222-8222-222222222222", Name: "关联分析", RoleKeys: []string{"tenant.analyst", "tenant.reader"}, Decision: Decision{Effect: EffectMask, Algorithm: AlgorithmSM3V1, InvalidValueEffect: EffectSuppress}}},
	}
	selection := RoleOptionSelection{PolicyID: rule.RoleOptions.PolicyID, AssessmentRevision: 1, PolicyRevision: 2, OptionID: rule.RoleOptions.Options[0].ID}
	for _, roles := range [][]string{{"tenant.reader"}, {"tenant.unrelated", "tenant.reader"}, {"tenant.analyst", "tenant.reader"}} {
		decision, err := rule.ResolveRoleOption(selection, roles)
		if err != nil || decision.Algorithm != AlgorithmSM3V1 {
			t.Fatalf("union decision=%+v err=%v", decision, err)
		}
		if !reflect.DeepEqual(rule.EffectiveDecision(SubjectReference{}, now), rule.Decision) {
			t.Fatal("role option changed default")
		}
	}
	for _, roles := range [][]string{nil, {"tenant.admin"}, {"tenant.other"}} {
		if _, err := rule.ResolveRoleOption(selection, roles); !errors.Is(err, ErrRoleOptionIneligible) {
			t.Fatalf("ineligible roles admitted: %v", err)
		}
	}
	for _, invalid := range []RoleOptionSelection{{}, {PolicyID: selection.PolicyID, AssessmentRevision: 2, PolicyRevision: 2, OptionID: selection.OptionID}, {PolicyID: selection.PolicyID, AssessmentRevision: 1, PolicyRevision: 1, OptionID: selection.OptionID}, {PolicyID: selection.PolicyID, AssessmentRevision: 1, PolicyRevision: 2, OptionID: "removed"}} {
		if _, err := rule.ResolveRoleOption(invalid, []string{"tenant.reader"}); !errors.Is(err, ErrRoleOptionUnavailable) {
			t.Fatalf("stale choice admitted: %v", err)
		}
	}
	subject := SubjectReference{Type: "user", ID: "33"}
	rule.Authorizations = []TemporaryAuthorization{{Subject: subject, Effect: EffectAllow, ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Minute)}}
	if rule.EffectiveDecision(subject, now).Effect != EffectAllow {
		t.Fatal("default exemption changed")
	}
	if decision, err := rule.ResolveRoleOption(selection, []string{"tenant.reader"}); err != nil || decision.Effect != EffectMask {
		t.Fatal("explicit option replaced by plaintext")
	}
	rule.Decision = Decision{Effect: EffectDeny}
	if _, err := rule.ResolveRoleOption(selection, []string{"tenant.reader"}); !errors.Is(err, ErrRoleOptionUnavailable) {
		t.Fatal("mandatory deny bypassed")
	}
}

func TestRoleOptionProtocolRejectsInvalidOptionsAndLegacyPayloads(t *testing.T) {
	option := RoleMaskingOption{ID: "22222222-2222-4222-8222-222222222222", Name: "统计", RoleKeys: []string{"tenant.reader"}, Decision: Decision{Effect: EffectMask, Algorithm: AlgorithmSM3V1, InvalidValueEffect: EffectSuppress}}
	for name, mutate := range map[string]func(*RoleMaskingOption){
		"allow":             func(o *RoleMaskingOption) { o.Decision.Effect = EffectAllow },
		"unknown algorithm": func(o *RoleMaskingOption) { o.Decision.Algorithm = "custom" },
		"empty roles":       func(o *RoleMaskingOption) { o.RoleKeys = nil },
		"duplicate roles":   func(o *RoleMaskingOption) { o.RoleKeys = []string{"tenant.reader", "tenant.reader"} },
		"invalid role":      func(o *RoleMaskingOption) { o.RoleKeys = []string{"管理员"} },
		"empty name":        func(o *RoleMaskingOption) { o.Name = "" },
	} {
		t.Run(name, func(t *testing.T) {
			copy := option
			mutate(&copy)
			if ValidateRoleMaskingOptions([]RoleMaskingOption{copy}, Decision{Effect: EffectMask}, "string") == nil {
				t.Fatal("invalid option accepted")
			}
		})
	}
	p := testProjection(time.Now().UTC())
	p.SchemaVersion = "addp.protection_projection/v4"
	if err := p.Seal(); err != nil {
		t.Fatal(err)
	}
	if p.Validate(time.Time{}) == nil {
		t.Fatal("v4 runtime accepted")
	}
	payload, _ := json.Marshal(p)
	if _, _, err := DecodePersistedProjectionForMigration(payload); err != nil {
		t.Fatal(err)
	}
	p.Rules[0].RoleOptions = &RoleMaskingOptions{PolicyID: "11111111-1111-4111-8111-111111111111", AssessmentRevision: 1, PolicyRevision: 1, Options: []RoleMaskingOption{option}}
	if err := p.Seal(); err != nil {
		t.Fatal(err)
	}
	payload, _ = json.Marshal(p)
	if _, _, err := DecodePersistedProjectionForMigration(payload); err == nil {
		t.Fatal("legacy option smuggling accepted")
	}
}
