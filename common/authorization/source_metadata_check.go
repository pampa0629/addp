package authorization

import (
	"time"

	"github.com/addp/common/engine/plugin"
)

// SourceMetadataCheckRequest carries candidates, never identity or permission
// evidence. The fixed Meta query operation determines the requested action.
type SourceMetadataCheckRequest struct {
	Targets []plugin.EngineCatalogPath `json:"targets"`
}

func (r SourceMetadataCheckRequest) Valid() bool {
	if len(r.Targets) == 0 || len(r.Targets) > 200 {
		return false
	}
	seen := make(map[string]bool, len(r.Targets))
	for _, target := range r.Targets {
		key, err := EncodeSharingTarget(target)
		if err != nil || seen[string(key)] {
			return false
		}
		seen[string(key)] = true
	}
	return true
}

type SourceMetadataTargetCheck struct {
	Target  plugin.EngineCatalogPath `json:"target"`
	Visible bool                     `json:"visible"`
}

// A current observation, not a cacheable Allow or an authorization to refresh.
type SourceMetadataCheck struct {
	ObservedAt time.Time                   `json:"observed_at"`
	Targets    []SourceMetadataTargetCheck `json:"targets"`
}

// Matches binds every decision to the original ordered candidates.
func (r *SourceMetadataCheck) Matches(request SourceMetadataCheckRequest) bool {
	if r == nil || r.ObservedAt.IsZero() || !request.Valid() || len(r.Targets) != len(request.Targets) {
		return false
	}
	for i, result := range r.Targets {
		actual, err := EncodeSharingTarget(result.Target)
		expected, expectedErr := EncodeSharingTarget(request.Targets[i])
		if err != nil || expectedErr != nil || string(actual) != string(expected) {
			return false
		}
	}
	return true
}

// SourceSearchCheck is a point-in-time observation for the fixed Manager search operation.
type SourceSearchCheck struct {
	ObservedAt time.Time                 `json:"observed_at"`
	Targets    []SourceSearchTargetCheck `json:"targets"`
}

type SourceSearchTargetCheck struct {
	SourceMetadataTargetCheck
	ContentReadable bool `json:"content_readable"`
}

func (r *SourceSearchCheck) Matches(request SourceMetadataCheckRequest) bool {
	if r == nil || len(r.Targets) != len(request.Targets) {
		return false
	}
	metadata := SourceMetadataCheck{ObservedAt: r.ObservedAt, Targets: make([]SourceMetadataTargetCheck, len(r.Targets))}
	for i, target := range r.Targets {
		if target.ContentReadable && !target.Visible {
			return false
		}
		metadata.Targets[i] = target.SourceMetadataTargetCheck
	}
	return metadata.Matches(request)
}
