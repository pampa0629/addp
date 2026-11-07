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
	file    *execution.ManagerFileReadScope
	fileGDB *execution.FileGDBExportReadScope
}

func NewManagerProfileSourceScope(scope *execution.ManagerProfileReadScope) *ManagerSourceReadScope {
	return &ManagerSourceReadScope{profile: scope.Clone()}
}

func NewManagerFileSourceScope(scope *execution.ManagerFileReadScope) *ManagerSourceReadScope {
	return &ManagerSourceReadScope{file: scope.Clone()}
}

func NewFileGDBExportSourceScope(scope *execution.FileGDBExportReadScope) *ManagerSourceReadScope {
	return &ManagerSourceReadScope{fileGDB: scope.Clone()}
}

func (s *ManagerSourceReadScope) Profile() *execution.ManagerProfileReadScope {
	if s == nil || s.file != nil || s.fileGDB != nil {
		return nil
	}
	return s.profile.Clone()
}

func (s *ManagerSourceReadScope) File() *execution.ManagerFileReadScope {
	if s == nil || s.profile != nil || s.fileGDB != nil {
		return nil
	}
	return s.file.Clone()
}

func (s *ManagerSourceReadScope) FileGDB() *execution.FileGDBExportReadScope {
	if s == nil || s.profile != nil || s.file != nil {
		return nil
	}
	return s.fileGDB.Clone()
}

func (s *ManagerSourceReadScope) Validate() error {
	switch {
	case s.Profile() != nil:
		return s.profile.Validate()
	case s.File() != nil:
		return s.file.Validate()
	case s.FileGDB() != nil:
		return s.fileGDB.Validate()
	default:
		return errors.New("exactly one Manager source scope is required")
	}
}

func (s *ManagerSourceReadScope) Targets() ([]plugin.EngineCatalogPath, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if s.profile != nil {
		return s.profile.ReadSet.Clone().Paths, nil
	}
	if s.fileGDB != nil {
		return []plugin.EngineCatalogPath{s.fileGDB.Clone().FileGDBSource.Binding.NativeTarget}, nil
	}
	primary, err := s.file.FileComposition.PrimaryTarget()
	return []plugin.EngineCatalogPath{primary}, err
}

func (s *ManagerSourceReadScope) Clone() *ManagerSourceReadScope {
	if s == nil {
		return nil
	}
	return &ManagerSourceReadScope{profile: s.profile.Clone(), file: s.file.Clone(), fileGDB: s.fileGDB.Clone()}
}

func (s ManagerSourceReadScope) MarshalJSON() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if s.profile != nil {
		return json.Marshal(s.profile)
	}
	if s.fileGDB != nil {
		return json.Marshal(s.fileGDB)
	}
	return json.Marshal(s.file)
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
	} else if _, ok := fields["filegdb_source"]; ok {
		if len(fields) != 2 || fields["config_digest"] == nil || fields["filegdb_source"] == nil {
			return errors.New("invalid FileGDB export source scope")
		}
		var scope execution.FileGDBExportReadScope
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&scope); err != nil {
			return err
		}
		candidate = NewFileGDBExportSourceScope(&scope)
	} else {
		var scope execution.ManagerFileReadScope
		if err := json.Unmarshal(raw, &scope); err != nil {
			return err
		}
		candidate = NewManagerFileSourceScope(&scope)
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	*s = *candidate
	return nil
}
