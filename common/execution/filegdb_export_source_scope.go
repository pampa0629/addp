package execution

import (
	"bytes"
	"encoding/json"
	"errors"
	"path"
	"reflect"
	"strings"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/infra/objectstore"
	"github.com/addp/common/resourcetree"
)

var errFileGDBExportSourceScope = errors.New("invalid FileGDB export source scope")

// FileGDBExportSource freezes the exact native DataItem approved for one
// Manager export. The engine version binds it to the same connection facts.
type FileGDBExportSource struct {
	EngineVersion string                     `json:"engine_version"`
	Binding       authorizationfacts.Binding `json:"binding"`
}

// FileGDBExportReadScope is stored on the existing Execution Authorization.
// Its digest binds the entire pending Transfer configuration.
type FileGDBExportReadScope struct {
	ConfigDigest  string              `json:"config_digest"`
	FileGDBSource FileGDBExportSource `json:"filegdb_source"`
}

func (s FileGDBExportSource) EngineType() string {
	parts := s.Binding.NativeTarget.Segments
	if len(parts) == 3 && parts[2].Term == plugin.EngineCatalogTermTable && parts[2].Kind == plugin.EngineCatalogKindTable {
		switch parts[1].Term {
		case plugin.EngineCatalogTermSchema:
			return "postgresql"
		case plugin.EngineCatalogTermDatabase:
			return "mysql"
		}
	}
	return ""
}

func (s FileGDBExportSource) Validate() error {
	if !positiveFileSourceVersion(s.EngineVersion) || s.EngineType() == "" || s.Binding.NativeTarget.EngineID > uint(1<<63-1) ||
		s.Binding.FileComposition != nil || s.Binding.Scope != nil || s.Binding.MetadataTarget != nil ||
		s.Binding.Validate() != nil {
		return errFileGDBExportSourceScope
	}
	return nil
}

func NewFileGDBExportReadScope(raw json.RawMessage) (*FileGDBExportReadScope, error) {
	config, digest, err := executionConfigObject(raw)
	if err != nil {
		return nil, errFileGDBExportSourceScope
	}
	value, ok := config["filegdb_source"].(map[string]any)
	if !ok || len(value) != 2 {
		return nil, errFileGDBExportSourceScope
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, errFileGDBExportSourceScope
	}
	scope := &FileGDBExportReadScope{ConfigDigest: digest}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&scope.FileGDBSource) != nil || scope.Validate() != nil {
		return nil, errFileGDBExportSourceScope
	}

	runtime, _ := config["runtime"].(map[string]any)
	load, _ := config["load"].(map[string]any)
	source, _ := config["source"].(map[string]any)
	target, _ := config["target"].(map[string]any)
	if runtime["boundary"] != "bounded" || load["mode"] != "snapshot" ||
		len(source) != 3 || source["data_type"] != "table" || source["representation"] != "native" ||
		target["data_type"] != "table" || target["representation"] != "encoded" || target["format"] != "filegdb" ||
		target["locator"] != nil {
		return nil, errFileGDBExportSourceScope
	}
	if transforms, exists := config["transforms"]; exists {
		if empty, ok := transforms.([]any); !ok || len(empty) != 0 {
			return nil, errFileGDBExportSourceScope
		}
	}
	native := scope.FileGDBSource.Binding.NativeTarget
	locator, _ := source["locator"].(string)
	loc, err := resourcetree.ParseURI(locator)
	if err != nil || loc.Type != resourcetree.TypeTable || loc.ToURI() != locator {
		return nil, errFileGDBExportSourceScope
	}
	resolved, err := resourcetree.EngineCatalogPathFromLocator(plugin.TabularCatalogModel(native.Segments[1].Term), loc)
	if err != nil || !reflect.DeepEqual(resolved, native) {
		return nil, errFileGDBExportSourceScope
	}
	parent, _ := target["parent_locator"].(string)
	u, err := objectstore.Parse(parent)
	name, _ := target["name"].(string)
	if err != nil || u.Area != objectstore.Manager || u.Type != "prefix" ||
		!strings.HasSuffix(name, ".gdb.zip") || name == ".gdb.zip" || name != path.Base(name) || strings.ContainsRune(name, '\\') {
		return nil, errFileGDBExportSourceScope
	}
	return scope, nil
}

func (s *FileGDBExportReadScope) Validate() error {
	if s == nil || !authorizationfacts.ValidItemFingerprint(s.ConfigDigest) || s.FileGDBSource.Validate() != nil {
		return errFileGDBExportSourceScope
	}
	return nil
}

func (s *FileGDBExportReadScope) Clone() *FileGDBExportReadScope {
	if s == nil {
		return nil
	}
	copy := *s
	path := &copy.FileGDBSource.Binding.NativeTarget
	path.Segments = append([]plugin.EngineCatalogSegment(nil), path.Segments...)
	return &copy
}
