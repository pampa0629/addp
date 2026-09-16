package plugin

import (
	"context"
	"testing"

	"github.com/addp/common/query/plan"
)

type uncertifiedAnalyticalResolver struct{ *MockPlugin }

func (p *uncertifiedAnalyticalResolver) ResolveCapabilities(context.Context, ConnectionInfo, EngineCapabilities) (EngineCapabilities, error) {
	return EngineCapabilities{Compute: &ComputeCapabilities{Query: &QueryCapability{Supported: true, ResultKinds: []string{"table"}, Analytical: &AnalyticalCapability{Supported: true, PlanVersions: []string{plan.SchemaVersion}, SemanticProfiles: []string{plan.SemanticProfile}}}}}, nil
}

func TestResolvedAnalyticalCapabilityRequiresCompiler(t *testing.T) {
	p := &uncertifiedAnalyticalResolver{&MockPlugin{TypeValue: "uncertified_analytical_resolver"}}
	Register(p)
	defer Unregister(p.Type())
	if encoded, err := GenerateResolvedCapabilities(t.Context(), &Engine{EngineType: p.Type()}); err == nil || encoded != "" {
		t.Fatalf("resolver without compiler published capability: %s %v", encoded, err)
	}
}

func TestAnalyticalCapabilityProjection(t *testing.T) {
	base := EngineCapabilities{Compute: &ComputeCapabilities{Query: &QueryCapability{Supported: true, ResultKinds: []string{"table"}}}}
	capability := AnalyticalCapability{Supported: true, PlanVersions: []string{plan.SchemaVersion}, SemanticProfiles: []string{plan.SemanticProfile}}
	resolved, err := WithAnalyticalCapability(base, capability)
	if err != nil {
		t.Fatal(err)
	}
	if base.Compute.Query.Analytical != nil {
		t.Fatal("mutated static template")
	}
	capability.PlanVersions[0] = "mutated"
	encoded, err := MarshalEngineCapabilities(resolved)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseEngineCapabilities(encoded)
	if err != nil || !parsed.Compute.Query.Analytical.Supported {
		t.Fatalf("roundtrip: %v", err)
	}
	revoked, err := WithAnalyticalCapability(resolved, AnalyticalCapability{})
	if err != nil || revoked.Compute.Query.Analytical.Supported || len(revoked.Compute.Query.Analytical.PlanVersions) != 0 {
		t.Fatalf("revocation: %v", err)
	}
	if !resolved.Compute.Query.Analytical.Supported {
		t.Fatal("mutated previous snapshot")
	}
	for name, value := range map[string]AnalyticalCapability{
		"false with versions": {PlanVersions: []string{plan.SchemaVersion}},
		"empty supported":     {Supported: true},
		"unknown version":     {Supported: true, PlanVersions: []string{"unknown"}, SemanticProfiles: []string{plan.SemanticProfile}},
		"duplicate version":   {Supported: true, PlanVersions: []string{plan.SchemaVersion, plan.SchemaVersion}, SemanticProfiles: []string{plan.SemanticProfile}},
		"unknown profile":     {Supported: true, PlanVersions: []string{plan.SchemaVersion}, SemanticProfiles: []string{"unknown"}},
		"duplicate profile":   {Supported: true, PlanVersions: []string{plan.SchemaVersion}, SemanticProfiles: []string{plan.SemanticProfile, plan.SemanticProfile}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := WithAnalyticalCapability(base, value); err == nil {
				t.Fatal("accepted invalid capability")
			}
		})
	}
	for _, raw := range []string{
		`{"compute":{"query":{"supported":false,"result_kinds":["table"],"analytical":{"supported":true,"plan_versions":["addp.query_plan/v1"],"semantic_profiles":["relational_analytics_v1"]}}}}`,
		`{"compute":{"query":{"supported":true,"result_kinds":["scalar"],"analytical":{"supported":true,"plan_versions":["addp.query_plan/v1"],"semantic_profiles":["relational_analytics_v1"]}}}}`,
		`{"compute":{"query":{"analytical":{"supported":false,"plan_versions":["addp.query_plan/v1"]}}}}`,
	} {
		if _, err := ParseEngineCapabilities(raw); err == nil {
			t.Fatal("accepted malformed wire capability")
		}
	}
}
