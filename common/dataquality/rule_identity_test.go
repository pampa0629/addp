package dataquality

import "testing"

func TestBackfillRuleKeyVector(t *testing.T) {
	t.Parallel()

	canonicalRule := []byte(`{"type": "not_null", "params": {}, "enabled": true, "message": "", "severity": "error"}`)
	ruleKey, err := BackfillRuleKey(9_800_000_001, 9_800_000_001, canonicalRule, 1)
	if err != nil {
		t.Fatalf("BackfillRuleKey() error = %v", err)
	}
	if ruleKey != "c0c3083c-9caf-8f5b-8f55-0811305fdee6" {
		t.Fatalf("BackfillRuleKey() = %q", ruleKey)
	}

	duplicateKey, err := BackfillRuleKey(9_800_000_001, 9_800_000_001, canonicalRule, 2)
	if err != nil {
		t.Fatalf("BackfillRuleKey() duplicate error = %v", err)
	}
	if duplicateKey == ruleKey {
		t.Fatal("duplicate occurrence must produce a distinct rule key")
	}
}

func TestBackfillRuleKeyRejectsInvalidIdentityInput(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		tenantID   int64
		elementID  int64
		canonical  []byte
		occurrence int
	}{
		{name: "tenant", tenantID: 0, elementID: 1, canonical: []byte(`{}`), occurrence: 1},
		{name: "element", tenantID: 1, elementID: 0, canonical: []byte(`{}`), occurrence: 1},
		{name: "canonical rule", tenantID: 1, elementID: 1, occurrence: 1},
		{name: "occurrence", tenantID: 1, elementID: 1, canonical: []byte(`{}`), occurrence: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := BackfillRuleKey(test.tenantID, test.elementID, test.canonical, test.occurrence); err == nil {
				t.Fatal("BackfillRuleKey() error = nil")
			}
		})
	}
}
