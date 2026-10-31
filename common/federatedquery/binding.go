package federatedquery

import (
	"fmt"
	"slices"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/selection"
	"github.com/addp/common/models"
)

// ExecutionBinding separates the selected compute target from the HTTP actor.
// This is masked control-plane discovery, not credential or source authorization.
type ExecutionBinding struct {
	Target         *models.EngineRuntimeDescriptor
	Runtime        *models.EngineRuntimeDescriptor
	Audience       string
	TargetEngineID uint
}

func federation(descriptor *models.EngineRuntimeDescriptor) (*plugin.QueryFederationCapability, error) {
	if descriptor == nil || descriptor.ID == 0 || !selection.IsAvailable(descriptor.AsEngine()) {
		return nil, fmt.Errorf("query execution engine is unavailable")
	}
	caps, err := selection.ParseCapabilities(descriptor.Capabilities)
	if err != nil || caps == nil || caps.EngineType != descriptor.EngineType || caps.Compute == nil || caps.Compute.Query == nil ||
		!caps.Compute.Query.Supported || !caps.Compute.Query.ReadOnly || caps.Compute.Query.Federation == nil || !caps.Compute.Query.Federation.Supported {
		return nil, fmt.Errorf("query federation capability is unavailable")
	}
	fed := caps.Compute.Query.Federation
	if fed.RuntimeAPI != "addp.query-runtime/v1" || fed.ExecutionAudience == "" ||
		strings.TrimSpace(fed.ExecutionAudience) != fed.ExecutionAudience {
		return nil, fmt.Errorf("query federation protocol or execution audience is invalid")
	}
	return fed, nil
}

func ResolveExecutionBinding(target *models.EngineRuntimeDescriptor, candidates []models.EngineRuntimeDescriptor) (*ExecutionBinding, error) {
	fed, err := federation(target)
	if err != nil {
		return nil, err
	}
	if len(fed.TargetEngineTypes) != 0 {
		return nil, fmt.Errorf("query runtime requires a concrete compute target")
	}
	if fed.RuntimeEngineType == "" {
		if !validEndpoint(target.RuntimeEndpoint) {
			return nil, fmt.Errorf("query runtime endpoint is invalid")
		}
		return &ExecutionBinding{Target: target, Runtime: target, Audience: fed.ExecutionAudience}, nil
	}
	if target.RuntimeEndpoint != nil {
		return nil, fmt.Errorf("delegated query target cannot expose or resolve its own runtime endpoint")
	}
	var runtime *models.EngineRuntimeDescriptor
	for i := range candidates {
		candidate := &candidates[i]
		if candidate.EngineType != fed.RuntimeEngineType || !candidate.IsBuiltin || candidate.LifecycleState != models.EngineLifecycleActive {
			continue
		}
		if runtime != nil {
			return nil, fmt.Errorf("query runtime binding is ambiguous")
		}
		runtime = candidate
	}
	actor, err := federation(runtime)
	if err != nil {
		return nil, err
	}
	if runtime.ID == target.ID || actor.RuntimeEngineType != "" || actor.ExecutionAudience != fed.ExecutionAudience ||
		!slices.Contains(actor.TargetEngineTypes, target.EngineType) || !validEndpoint(runtime.RuntimeEndpoint) {
		return nil, fmt.Errorf("query runtime does not accept this compute target")
	}
	for _, sourceType := range fed.SourceEngineTypes {
		if !slices.Contains(actor.SourceEngineTypes, sourceType) {
			return nil, fmt.Errorf("query runtime source connector mismatch")
		}
	}
	for _, format := range fed.ObjectFormats {
		if !slices.Contains(actor.ObjectFormats, format) {
			return nil, fmt.Errorf("query runtime object format mismatch")
		}
	}
	return &ExecutionBinding{Target: target, Runtime: runtime, Audience: fed.ExecutionAudience, TargetEngineID: target.ID}, nil
}

func validEndpoint(endpoint *models.EngineRuntimeEndpoint) bool {
	return endpoint != nil && (endpoint.Protocol == "http" || endpoint.Protocol == "https") &&
		endpoint.Host != "" && strings.TrimSpace(endpoint.Host) == endpoint.Host &&
		!strings.ContainsAny(endpoint.Host, "/@?# \t\r\n") && endpoint.Port > 0 && endpoint.Port <= 65535
}
