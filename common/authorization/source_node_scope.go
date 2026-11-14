package authorization

import (
	"errors"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
)

// SourceNodeScope is an immutable Grant range, not a frozen list of item identities.
type SourceNodeScope struct {
	Target    plugin.EngineCatalogPath `json:"target"`
	Recursive bool                     `json:"recursive"`
}

func (s SourceNodeScope) Validate() error {
	if err := (authorizationfacts.NodeIdentityQuery{Target: s.Target}).Validate(); err != nil {
		return err
	}
	if s.Target.Segments[0].Term == "service" && len(s.Target.Segments) == 1 && !s.Recursive {
		return errors.New("MinIO service root requires recursive scope")
	}
	return nil
}

// Covers compares trusted typed paths only. Callers must independently establish
// the live tenant/engine, complete DataItem boundary and current Grant lifecycle.
func (s SourceNodeScope) Covers(target plugin.EngineCatalogPath) bool {
	if s.Validate() != nil || target.Version != s.Target.Version || target.EngineID != s.Target.EngineID ||
		len(target.Segments) <= len(s.Target.Segments) || (!s.Recursive && len(target.Segments) != len(s.Target.Segments)+1) {
		return false
	}
	// A whole DataItem has a directory or prefix target. Its identity and
	// complete composition are proved by the caller, not inferred here.
	if (target.Segments[0].Term == plugin.EngineCatalogTermRoot && target.Segments[len(target.Segments)-1].Kind == plugin.EngineCatalogKindDirectory) ||
		(target.Segments[0].Term == plugin.EngineCatalogTermService && target.Segments[len(target.Segments)-1].Kind == plugin.EngineCatalogKindPrefix) {
		if !authorizationfacts.ValidNodeIdentityTarget(target) {
			return false
		}
	} else if _, err := plugin.NewQueryReadSet(target); err != nil {
		return false
	}
	for i, segment := range s.Target.Segments {
		if target.Segments[i] != segment {
			return false
		}
	}
	return true
}
