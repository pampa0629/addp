package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/addp/quality/internal/models"
)

const countAssertion = `{"op":"eq","args":[{"op":"field","field":"actual"},{"op":"count_distinct","relation":"detail","field":"item","where":{"op":"eq","args":[{"op":"field","field":"key"},{"op":"field","relation":"detail","field":"key"}]}}]}`

func assertionPlanRule(t *testing.T, assertion string) PlanRule {
	t.Helper()
	params := json.RawMessage(`{"table":"people","fields":{"actual":"actual","key":"person_id"},"relations":{"detail":{"table":"details","fields":{"key":"person_id","item":"activity_id"}}},"assertion":` + assertion + `}`)
	return PlanRule{RuleKey: "00000000-0000-4000-8000-000000000001", Type: "relational_assertion", Severity: "error", Params: params}
}

func TestAssertionDefinitionAndBindings(t *testing.T) {
	definition, err := resolvedDefinition(models.RuleContent{Type: "relational_assertion", Params: json.RawMessage(`{"assertion":` + countAssertion + `}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err = validatePlanRule(definition, map[string]struct{}{"target": {}, "reference": {}}); err != nil {
		t.Fatal(err)
	}
	rule := assertionPlanRule(t, countAssertion)
	aliases := map[string]struct{}{"people": {}, "details": {}}
	if err := validatePlanRule(rule, aliases); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*planAssertionParams){
		func(p *planAssertionParams) { delete(p.Fields, "key") },
		func(p *planAssertionParams) { p.Fields["unused"] = "other" },
		func(p *planAssertionParams) { p.Relations["detail"] = models.RelationBinding{Table: "unbound"} },
		func(p *planAssertionParams) { p.Relations["extra"] = p.Relations["detail"] },
		func(p *planAssertionParams) { p.Relations["detail"].Fields["key"] = " " },
	} {
		var p planAssertionParams
		_ = json.Unmarshal(rule.Params, &p)
		mutate(&p)
		if err := p.validate(aliases); err == nil {
			t.Fatalf("accepted invalid bindings: %#v", p)
		}
	}
	if target := ruleTarget(rule); target.Table != "people" || strings.Join(target.Columns, ",") != "actual,person_id" {
		t.Fatalf("target=%#v", target)
	}
}

func TestAssertionCompilerQuotesIdentifiersAndBindsValues(t *testing.T) {
	rule := PlanRule{Type: "relational_assertion", Params: json.RawMessage(`{"table":"people","fields":{"key":"odd\"column"},"assertion":{"op":"eq","args":[{"op":"field","field":"key"},{"op":"value","value":"'; DROP TABLE x;--"}]}}`)}
	aliases := map[string]validationReadItem{"people": {EngineID: 1, Locator: "addp://engine/1/path/public/people?type=table", Columns: []validationColumn{{Name: `odd"column`}}}}
	compiled, err := compilePlanRule(rule, aliases)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(compiled.SQL, "DROP") || !strings.Contains(compiled.SQL, `q0."odd""column"`) || len(compiled.Args) != 1 || compiled.Args[0] != "'; DROP TABLE x;--" {
		t.Fatalf("unsafe SQL: %#v", compiled)
	}
	aliases["people"] = validationReadItem{EngineID: 1, Locator: "addp://engine/1/path/public/people?type=table"}
	if _, err := compilePlanRule(rule, aliases); err == nil {
		t.Fatal("accepted absent physical column")
	}
}
