package models

import (
	"encoding/json"
	"testing"
)

func TestPlanTargetsResolveAndCanonicalScope(t *testing.T) {
	defaults := json.RawMessage(`[{"alias":"a","locator":"addp://engine/12/path/east/orders?type=table"},{"alias":"b","locator":""}]`)
	if _, _, err := ResolvePlanTargets(defaults, PlanRunRequest{}); err == nil {
		t.Fatal("missing required input accepted")
	}
	request := PlanRunRequest{TableBindings: map[string]string{"b": "addp://engine/12/path/east/people?type=table"}}
	bindings, key, err := ResolvePlanTargets(defaults, request)
	if err != nil || len(key) != 64 {
		t.Fatalf("%s %v", key, err)
	}
	bindings[0], bindings[1] = bindings[1], bindings[0]
	bindings[0].Locator += "&item_id=32"
	if reordered, err := PlanTargetKey(bindings); err != nil || reordered != key {
		t.Fatalf("cache hint or ordering changed identity: %s %v", reordered, err)
	}
	for _, overrides := range []map[string]string{
		{"a": ""}, {"unknown": "addp://engine/12/path/east/orders?type=table"},
		{"b": "addp://engine/13/path/east/people?type=table"},
		{"b": "addp://engine/12/path/east/orders?type=table&item_id=12"},
	} {
		if _, _, err := ResolvePlanTargets(defaults, PlanRunRequest{TableBindings: overrides}); err == nil {
			t.Fatalf("accepted invalid override %v", overrides)
		}
	}
	request.TableBindings["b"] = "addp://engine/12/path/west/people?type=table"
	_, different, err := ResolvePlanTargets(defaults, request)
	if err != nil || different == key {
		t.Fatal("reference-table scope was not isolated")
	}
	// A reusable plan is not tied to its default engine. Replace the entire
	// target set to check another region on another PostgreSQL instance.
	rebound, _, err := ResolvePlanTargets(defaults, PlanRunRequest{TableBindings: map[string]string{
		"a": "addp://engine/13/path/west/orders?type=table",
		"b": "addp://engine/13/path/west/people?type=table",
	}})
	if err != nil || rebound[0].Locator != "addp://engine/13/path/west/orders?type=table" {
		t.Fatalf("full engine override rejected: %v", err)
	}
	if string(defaults) != `[{"alias":"a","locator":"addp://engine/12/path/east/orders?type=table"},{"alias":"b","locator":""}]` {
		t.Fatal("default definitions mutated")
	}
}
