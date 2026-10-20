package iam

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/jsonmap"
)

// ManagerSourceReadScope is the closed sum stored in the single canonical
// source_read_scope column. Each purpose retains its strict common wire shape;
// no row can carry two scopes or fall back to generic engine access.
type ManagerSourceReadScope struct {
	profile *execution.ManagerProfileReadScope
	model3d *execution.ManagerModel3DReadScope
}

func NewManagerProfileSourceScope(scope *execution.ManagerProfileReadScope) *ManagerSourceReadScope {
	return &ManagerSourceReadScope{profile: scope.Clone()}
}

func NewManagerModel3DSourceScope(scope *execution.ManagerModel3DReadScope) *ManagerSourceReadScope {
	return &ManagerSourceReadScope{model3d: scope.Clone()}
}

func (s *ManagerSourceReadScope) Profile() *execution.ManagerProfileReadScope {
	if s == nil || s.model3d != nil {
		return nil
	}
	return s.profile.Clone()
}

func (s *ManagerSourceReadScope) Model3D() *execution.ManagerModel3DReadScope {
	if s == nil || s.profile != nil {
		return nil
	}
	return s.model3d.Clone()
}

func (s *ManagerSourceReadScope) Validate() error {
	if s == nil || (s.profile == nil) == (s.model3d == nil) {
		return errors.New("exactly one Manager source scope is required")
	}
	if s.profile != nil {
		return s.profile.Validate()
	}
	return s.model3d.Validate()
}

func (s *ManagerSourceReadScope) Targets() ([]plugin.EngineCatalogPath, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if s.profile != nil {
		return s.profile.ReadSet.Clone().Paths, nil
	}
	primary, err := s.model3d.FileComposition.PrimaryTarget()
	return []plugin.EngineCatalogPath{primary}, err
}

func (s *ManagerSourceReadScope) Clone() *ManagerSourceReadScope {
	if s == nil {
		return nil
	}
	return &ManagerSourceReadScope{profile: s.profile.Clone(), model3d: s.model3d.Clone()}
}

func (s ManagerSourceReadScope) MarshalJSON() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if s.profile != nil {
		return json.Marshal(s.profile)
	}
	return json.Marshal(s.model3d)
}

func (s *ManagerSourceReadScope) UnmarshalJSON(raw []byte) error {
	fields, err := jsonmap.DecodeObjectStrict(raw, execution.MaxExecutionConfigBytes, 64)
	if err != nil {
		return err
	}
	var candidate *ManagerSourceReadScope
	if _, ok := fields["read_set"]; ok {
		if len(fields) != 2 || fields["config_digest"] == nil || fields["read_set"] == nil {
			return errors.New("invalid Manager profile source scope")
		}
		var scope execution.ManagerProfileReadScope
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&scope); err != nil {
			return err
		}
		candidate = NewManagerProfileSourceScope(&scope)
	} else {
		var scope execution.ManagerModel3DReadScope
		if err := json.Unmarshal(raw, &scope); err != nil {
			return err
		}
		candidate = NewManagerModel3DSourceScope(&scope)
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	*s = *candidate
	return nil
}
