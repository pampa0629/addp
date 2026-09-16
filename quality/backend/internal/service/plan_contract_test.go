package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidatePlanContractRejectsUnknownFieldsAndDuplicateBindings(t *testing.T) {
	document := json.RawMessage(`{"schema_version":"addp.quality.plan-rules/v1","rules":[{"rule_key":"f3889a4a-1675-4623-b6e3-773f9125a04d","type":"not_null","severity":"error","params":{"table":"orders","column":"id","sql":"drop table x"}}]}`)
	_, err := validatePlanContract([]PlanTableBinding{{Alias: "orders", Locator: "addp://engine/12/path/public/table_3?type=table"}}, document)
	if err == nil || !strings.Contains(err.Error(), "not_null params are invalid") {
		t.Fatalf("unknown params field error = %v", err)
	}

	valid := gateTestDocument(`{"table":"orders","column":"id"}`, "not_null")
	_, err = validatePlanContract([]PlanTableBinding{{Alias: "orders", Locator: "addp://engine/12/path/public/table_3?type=table"}, {Alias: "orders_copy", Locator: "addp://engine/12/path/public/table_3?type=table"}}, valid)
	if err == nil || !strings.Contains(err.Error(), "physical table binding is duplicated") {
		t.Fatalf("duplicate logical table error = %v", err)
	}
}

func TestValidatePlanAllowedValues(t *testing.T) {
	binding := []PlanTableBinding{{Alias: "orders", Locator: "addp://engine/12/path/public/table_3?type=table"}}
	valid := gateTestDocument(`{"table":"orders","column":"status","values":["enabled","disabled"]}`, "allowed_values")
	if _, err := validatePlanContract(binding, valid); err != nil {
		t.Fatalf("valid allowed_values rejected: %v", err)
	}
	for name, params := range map[string]string{
		"empty":     `{"table":"orders","column":"status","values":[]}`,
		"blank":     `{"table":"orders","column":"status","values":[""]}`,
		"duplicate": `{"table":"orders","column":"status","values":["enabled","enabled"]}`,
		"unknown":   `{"table":"orders","column":"status","values":["enabled"],"sql":"drop table orders"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := validatePlanContract(binding, gateTestDocument(params, "allowed_values")); err == nil || !strings.Contains(err.Error(), "allowed_values params are invalid") {
				t.Fatalf("invalid allowed_values error = %v", err)
			}
		})
	}
}

func TestCompilePlanUsesOnlyReadContextIdentifiers(t *testing.T) {
	config := &planExecutionConfig{
		TableBindings: []PlanTableBinding{{Alias: "orders", Locator: "addp://engine/12/path/public/table_3?type=table"}, {Alias: "customers", Locator: "addp://engine/12/path/public/table_7?type=table"}},
		Rules: PlanRuleDocument{Rules: []PlanRule{
			{RuleKey: "d02be89b-40ca-46f6-a624-5577aa027791", Type: "allowed_values", Severity: "error", Params: json.RawMessage(`{"table":"orders","column":"customer_id","values":["customer-1","customer-2"]}`)},
			{RuleKey: "f3889a4a-1675-4623-b6e3-773f9125a04d", Type: "unique_key", Severity: "error", Params: json.RawMessage(`{"table":"orders","columns":["id"]}`)},
			{RuleKey: "0cd81b20-8fe8-4fce-a77e-c4c385175d41", Type: "foreign_key", Severity: "error", Params: json.RawMessage(`{"table":"orders","columns":["customer_id"],"reference_table":"customers","reference_columns":["id"]}`)},
		}},
	}
	readContext := &validationReadContext{Items: []validationReadItem{
		{EngineID: 12, Locator: "addp://engine/12/path/public/table_3?type=table", Columns: []validationColumn{{Name: "id"}, {Name: "customer_id"}}},
		{EngineID: 12, Locator: "addp://engine/12/path/public/table_7?type=table", Columns: []validationColumn{{Name: "id"}}},
	}}
	compiled, _, err := compilePlan(config, readContext)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled) != 3 || !strings.Contains(compiled[0].SQL, `"public"."table_3"`) || !strings.Contains(compiled[1].SQL, `"public"."table_3"`) || !strings.Contains(compiled[2].SQL, `"public"."table_7"`) {
		t.Fatalf("compiled rules = %#v", compiled)
	}
	if !strings.Contains(compiled[0].SQL, `"customer_id"::text NOT IN ($1, $2)`) || len(compiled[0].Args) != 2 || compiled[0].Args[0] != "customer-1" || compiled[0].Args[1] != "customer-2" {
		t.Fatalf("compiled allowed_values = %#v", compiled[0])
	}

	config.Rules.Rules[1].Params = json.RawMessage(`{"table":"orders","columns":["missing"]}`)
	if _, _, err := compilePlan(config, readContext); err == nil || !strings.Contains(err.Error(), "not present") {
		t.Fatalf("missing column error = %v", err)
	}
}

func TestGateRowCountBounds(t *testing.T) {
	exact := int64(4)
	if !planRowCountPassed(4, planRowCountParams{Exact: &exact}) || planRowCountPassed(5, planRowCountParams{Exact: &exact}) {
		t.Fatal("exact row count evaluation is invalid")
	}
	min, max := int64(2), int64(5)
	if !planRowCountPassed(3, planRowCountParams{Min: &min, Max: &max}) || planRowCountPassed(6, planRowCountParams{Min: &min, Max: &max}) {
		t.Fatal("range row count evaluation is invalid")
	}
}

func gateTestDocument(params, ruleType string) json.RawMessage {
	return json.RawMessage(`{"schema_version":"addp.quality.plan-rules/v1","rules":[{"rule_key":"f3889a4a-1675-4623-b6e3-773f9125a04d","type":"` + ruleType + `","severity":"error","params":` + params + `}]}`)
}

func TestPlanRejectsCrossEngineAndDuplicatePhysicalIdentity(t *testing.T) {
	doc := gateTestDocument(`{"table":"orders","column":"id"}`, "not_null")
	for _, other := range []string{"addp://engine/13/path/public/table_3?type=table", "addp://engine/12/path/public/table_3?type=table&item_id=42"} {
		_, err := validatePlanContract([]PlanTableBinding{{Alias: "orders", Locator: "addp://engine/12/path/public/table_3?type=table"}, {Alias: "other", Locator: other}}, doc)
		if err == nil {
			t.Fatalf("accepted ambiguous binding %s", other)
		}
	}
}
