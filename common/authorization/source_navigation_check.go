package authorization

import (
	"encoding/json"
	"reflect"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
)

// SourceNavigationCheckRequest asks about existing navigation nodes, never content.
type SourceNavigationCheckRequest struct {
	Targets []plugin.EngineCatalogPath `json:"targets"`
}

func (r SourceNavigationCheckRequest) Valid() bool {
	if len(r.Targets) == 0 || len(r.Targets) > 200 {
		return false
	}
	seen := map[string]bool{}
	for _, target := range r.Targets {
		if !authorizationfacts.ValidNodeIdentityTarget(target) {
			return false
		}
		raw, _ := json.Marshal(target)
		if seen[string(raw)] {
			return false
		}
		seen[string(raw)] = true
	}
	return true
}

func (r *SourceMetadataCheck) MatchesNavigation(q SourceNavigationCheckRequest) bool {
	if r == nil || r.ObservedAt.IsZero() || !q.Valid() || len(r.Targets) != len(q.Targets) {
		return false
	}
	for i, target := range r.Targets {
		if !reflect.DeepEqual(target.Target, q.Targets[i]) {
			return false
		}
	}
	return true
}

// NavigationContains uses typed segments, including equality. It does not grant
// access to data or establish that a node or DataItem exists.
func NavigationContains(parent, child plugin.EngineCatalogPath) bool {
	if parent.EngineID != child.EngineID || parent.Version != child.Version || len(parent.Segments) > len(child.Segments) {
		return false
	}
	for i, part := range parent.Segments {
		if part != child.Segments[i] {
			return false
		}
	}
	return true
}
