package iam

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/jsonmap"
)

// SourceReadScope is the closed sum stored in the single canonical
// source_read_scope column. Each purpose retains its strict common wire shape;
// no row can carry two scopes or fall back to generic engine access.
type SourceReadScope struct {
	profile *execution.ManagerProfileReadScope
	file    *execution.ManagerFileReadScope
	fileGDB *execution.FileGDBExportReadScope
	object  *execution.TransferObjectReadScope
}

func NewManagerProfileSourceScope(scope *execution.ManagerProfileReadScope) *SourceReadScope {
	return &SourceReadScope{profile: scope.Clone()}
}

func NewManagerFileSourceScope(scope *execution.ManagerFileReadScope) *SourceReadScope {
	return &SourceReadScope{file: scope.Clone()}
}

func NewFileGDBExportSourceScope(scope *execution.FileGDBExportReadScope) *SourceReadScope {
	return &SourceReadScope{fileGDB: scope.Clone()}
}

func (s *SourceReadScope) Profile() *execution.ManagerProfileReadScope {
	if s == nil || s.file != nil || s.fileGDB != nil || s.object != nil {
		return nil
	}
	return s.profile.Clone()
}

func (s *SourceReadScope) File() *execution.ManagerFileReadScope {
	if s == nil || s.profile != nil || s.fileGDB != nil || s.object != nil {
		return nil
	}
	return s.file.Clone()
}

func (s *SourceReadScope) FileGDB() *execution.FileGDBExportReadScope {
	if s == nil || s.profile != nil || s.file != nil || s.object != nil {
		return nil
	}
	return s.fileGDB.Clone()
}

func NewTransferObjectSourceScope(scope *execution.TransferObjectReadScope) *SourceReadScope {
	return &SourceReadScope{object: scope.Clone()}
}
func (s *SourceReadScope) Object() *execution.TransferObjectReadScope {
	if s == nil || s.profile != nil || s.file != nil || s.fileGDB != nil {
		return nil
	}
	return s.object.Clone()
}

func (s *SourceReadScope) Validate() error {
	switch {
	case s.Object() != nil:
		return s.object.Validate()
	case s.Profile() != nil:
		return s.profile.Validate()
	case s.File() != nil:
		return s.file.Validate()
	case s.FileGDB() != nil:
		return s.fileGDB.Validate()
	default:
		return errors.New("exactly one execution source scope is required")
	}
}

func (s *SourceReadScope) Targets() ([]plugin.EngineCatalogPath, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if s.object != nil {
		return []plugin.EngineCatalogPath{s.object.Clone().ObjectSource.Binding.NativeTarget}, nil
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

func (s *SourceReadScope) Clone() *SourceReadScope {
	if s == nil {
		return nil
	}
	return &SourceReadScope{profile: s.profile.Clone(), file: s.file.Clone(), fileGDB: s.fileGDB.Clone(), object: s.object.Clone()}
}

func (s SourceReadScope) MarshalJSON() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if s.object != nil {
		return json.Marshal(s.object)
	}
	if s.profile != nil {
		return json.Marshal(s.profile)
	}
	if s.fileGDB != nil {
		return json.Marshal(s.fileGDB)
	}
	return json.Marshal(s.file)
}

func (s *SourceReadScope) UnmarshalJSON(raw []byte) error {
	fields, err := jsonmap.DecodeObjectStrict(raw, execution.MaxExecutionConfigBytes, 64)
	if err != nil {
		return err
	}
	var candidate *SourceReadScope
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
	} else if _, ok := fields["object_source"]; ok {
		if len(fields) != 3 || fields["config_digest"] == nil || fields["target_engine_id"] == nil {
			return errors.New("invalid Transfer object source scope")
		}
		var scope execution.TransferObjectReadScope
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&scope); err != nil {
			return err
		}
		candidate = NewTransferObjectSourceScope(&scope)
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
