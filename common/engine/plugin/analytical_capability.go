package plugin

import (
	"fmt"

	"github.com/addp/common/query/plan"
)

// Validate checks the public plan/profile contract, independent of engine type.
func (c AnalyticalCapability) Validate() error {
	if !c.Supported {
		if len(c.PlanVersions) != 0 || len(c.SemanticProfiles) != 0 {
			return fmt.Errorf("unsupported analytical capability must have empty versions and profiles")
		}
		return nil
	}
	if len(c.PlanVersions) != 1 || c.PlanVersions[0] != plan.SchemaVersion || len(c.SemanticProfiles) != 1 || c.SemanticProfiles[0] != plan.SemanticProfile {
		return fmt.Errorf("analytical capability requires the recognized unique plan version and semantic profile")
	}
	return nil
}

func validateAnalyticalQueryCapability(compute *ComputeCapabilities) error {
	if compute == nil || compute.Query == nil || compute.Query.Analytical == nil {
		return nil
	}
	q := compute.Query
	if err := q.Analytical.Validate(); err != nil {
		return err
	}
	if q.Analytical.Supported && (!q.Supported || !Contains(q.ResultKinds, "table")) {
		return fmt.Errorf("analytical capability requires supported tabular queries")
	}
	return nil
}

// WithAnalyticalCapability projects a certified result without mutating a
// shared static template or keeping a previous instance's support conclusion.
func WithAnalyticalCapability(base EngineCapabilities, capability AnalyticalCapability) (EngineCapabilities, error) {
	if base.Compute == nil || base.Compute.Query == nil {
		return EngineCapabilities{}, ErrAnalyticalUnsupported
	}
	compute, query := *base.Compute, *base.Compute.Query
	capability.PlanVersions = append([]string{}, capability.PlanVersions...)
	capability.SemanticProfiles = append([]string{}, capability.SemanticProfiles...)
	query.Analytical = &capability
	compute.Query = &query
	base.Compute = &compute
	if err := validateAnalyticalQueryCapability(base.Compute); err != nil {
		return EngineCapabilities{}, err
	}
	return base, nil
}
