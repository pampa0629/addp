package federatedquery

import (
	"encoding/json"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/selection"
	"github.com/addp/common/models"
)

func bindingDescriptor(t *testing.T, id uint, engineType, audience, runtimeType string, targets []string) models.EngineRuntimeDescriptor {
	t.Helper()
	caps := plugin.NewFederatedQueryCapabilities(engineType, "addp.query-runtime/v1", []string{"postgresql", "mysql"}, nil)
	fed := caps.Compute.Query.Federation
	fed.ExecutionAudience, fed.RuntimeEngineType, fed.TargetEngineTypes = audience, runtimeType, targets
	value, err := json.Marshal(caps)
	if err != nil {
		t.Fatal(err)
	}
	encoded := models.JSONString(value)
	d := models.EngineRuntimeDescriptor{ID: id, EngineType: engineType, Capabilities: &encoded,
		LifecycleState: models.EngineLifecycleActive, ConnectionStatus: models.EngineConnectionOnline}
	if runtimeType == "" {
		d.RuntimeEndpoint = &models.EngineRuntimeEndpoint{Protocol: "http", Host: "runtime", Port: 8098}
	}
	return d
}

func TestExecutionBindingSeparatesArbitraryComputeTargetsFromBuiltinRuntime(t *testing.T) {
	target := bindingDescriptor(t, 41, "cluster_type", "compute_audience", "runtime_type", nil)
	runtime := bindingDescriptor(t, 92, "runtime_type", "compute_audience", "", []string{"cluster_type"})
	runtime.IsBuiltin = true
	binding, err := ResolveExecutionBinding(&target, []models.EngineRuntimeDescriptor{runtime})
	if err != nil || binding.Target.ID != 41 || binding.Runtime.ID != 92 || binding.TargetEngineID != 41 || binding.Audience != "compute_audience" {
		t.Fatalf("binding=%+v error=%v", binding, err)
	}
	if !selection.IsSelectionOptionForComputeEntrypoint(target.AsEngine(), "query") || selection.IsSelectionOptionForComputeEntrypoint(runtime.AsEngine(), "query") {
		t.Fatal("query selector must show the compute target, not its runtime requiring a target")
	}
	if !selection.SupportsComputeEntrypoint(runtime.AsEngine(), "query") {
		t.Fatal("runtime retains its query protocol capability")
	}
	if selection.HasStorageCapability(target.AsEngine()) || selection.HasStorageCapability(runtime.AsEngine()) {
		t.Fatal("compute role must not create storage capability")
	}
}

func TestExecutionBindingSelfHostedRuntime(t *testing.T) {
	target := bindingDescriptor(t, 8, "duckdb", "duckdb", "", nil)
	binding, err := ResolveExecutionBinding(&target, nil)
	if err != nil || binding.Runtime.ID != 8 || binding.TargetEngineID != 0 {
		t.Fatalf("binding=%+v error=%v", binding, err)
	}
}

func TestExecutionBindingUsesRolesRatherThanRequiringDifferentTypeNames(t *testing.T) {
	target := bindingDescriptor(t, 41, "compute_type", "compute_audience", "compute_type", nil)
	runtime := bindingDescriptor(t, 92, "compute_type", "compute_audience", "", []string{"compute_type"})
	runtime.IsBuiltin = true
	binding, err := ResolveExecutionBinding(&target, []models.EngineRuntimeDescriptor{target, runtime})
	if err != nil || binding.Runtime.ID != 92 || binding.TargetEngineID != 41 {
		t.Fatalf("binding=%+v error=%v", binding, err)
	}
}

func TestExecutionBindingRejectsMissingAmbiguousOrUnqualifiedActors(t *testing.T) {
	for _, scenario := range []string{"missing", "duplicate", "offline", "disabled", "not_builtin", "audience", "target_type", "source_type", "bad_endpoint", "target_endpoint", "runtime_selected", "missing_audience"} {
		t.Run(scenario, func(t *testing.T) {
			target := bindingDescriptor(t, 41, "cluster_type", "compute_audience", "runtime_type", nil)
			runtime := bindingDescriptor(t, 92, "runtime_type", "compute_audience", "", []string{"cluster_type"})
			runtime.IsBuiltin = true
			candidates := []models.EngineRuntimeDescriptor{runtime}
			switch scenario {
			case "missing":
				candidates = nil
			case "duplicate":
				duplicate := runtime
				duplicate.ID = 93
				candidates = append(candidates, duplicate)
			case "offline":
				candidates[0].ConnectionStatus = models.EngineConnectionOffline
			case "disabled":
				candidates[0].LifecycleState = "disabled"
			case "not_builtin":
				candidates[0].IsBuiltin = false
			case "audience":
				candidates[0] = bindingDescriptor(t, 92, "runtime_type", "wrong", "", []string{"cluster_type"})
				candidates[0].IsBuiltin = true
			case "target_type":
				candidates[0] = bindingDescriptor(t, 92, "runtime_type", "compute_audience", "", []string{"other"})
				candidates[0].IsBuiltin = true
			case "source_type":
				target = bindingDescriptor(t, 41, "cluster_type", "compute_audience", "runtime_type", nil)
				caps, _ := selection.ParseCapabilities(target.Capabilities)
				caps.Compute.Query.Federation.SourceEngineTypes = []string{"unsupported"}
				value, _ := json.Marshal(caps)
				encoded := models.JSONString(value)
				target.Capabilities = &encoded
			case "bad_endpoint":
				candidates[0].RuntimeEndpoint.Port = 0
			case "target_endpoint":
				target.RuntimeEndpoint = runtime.RuntimeEndpoint
			case "runtime_selected":
				target = runtime
			case "missing_audience":
				target = bindingDescriptor(t, 41, "cluster_type", "", "runtime_type", nil)
			}
			if binding, err := ResolveExecutionBinding(&target, candidates); err == nil || binding != nil {
				t.Fatalf("accepted invalid %s: %+v", scenario, binding)
			}
		})
	}
}
