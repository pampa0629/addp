package service

import (
	"testing"

	"github.com/addp/common/dataquality"
)

func TestNormalizeQualityRulesDefaultsToVersionedEmptyDocument(t *testing.T) {
	rules, err := normalizeQualityRules(nil)
	if err != nil {
		t.Fatalf("normalizeQualityRules(nil) error = %v", err)
	}
	if rules["schema_version"] != dataquality.RulesSchemaVersion {
		t.Fatalf("schema_version = %v", rules["schema_version"])
	}
	items, ok := rules["rules"].([]interface{})
	if !ok || len(items) != 0 {
		t.Fatalf("rules = %#v, want empty array", rules["rules"])
	}
}

func TestNormalizeQualityRulesRejectsLegacyAndRemovedRuleTypes(t *testing.T) {
	tests := []struct {
		name  string
		value map[string]interface{}
	}{
		{name: "legacy array wrapper", value: map[string]interface{}{"rules": []interface{}{}}},
		{name: "custom rule", value: qualityRuleDocumentForTest("custom")},
		{name: "data type rule", value: qualityRuleDocumentForTest("data_type")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := normalizeQualityRules(tt.value); err == nil {
				t.Fatalf("normalizeQualityRules(%#v) error = nil", tt.value)
			}
		})
	}
}

func TestNormalizeQualityRulesAcceptsCanonicalRule(t *testing.T) {
	rules, err := normalizeQualityRules(qualityRuleDocumentForTest("not_null"))
	if err != nil {
		t.Fatalf("normalizeQualityRules() error = %v", err)
	}
	document, err := dataquality.FromValue(rules)
	if err != nil {
		t.Fatalf("normalized document error = %v", err)
	}
	if len(document.Rules) != 1 || document.Rules[0].Type != dataquality.RuleTypeNotNull {
		t.Fatalf("normalized document = %#v", document)
	}
}

func qualityRuleDocumentForTest(ruleType string) map[string]interface{} {
	return map[string]interface{}{
		"schema_version": dataquality.RulesSchemaVersion,
		"rules": []interface{}{map[string]interface{}{
			"type": ruleType, "enabled": true, "severity": "error", "message": "required", "params": map[string]interface{}{},
		}},
	}
}
