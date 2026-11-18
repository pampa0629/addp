package execution

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/jsonmap"
	"github.com/addp/common/resourcetree"
)

var errTransferObjectScope = errors.New("invalid Transfer object source scope")

// TransferObjectSource freezes one native single-object boundary and its engine version.
type TransferObjectSource struct {
	EngineVersion string                     `json:"engine_version"`
	Binding       authorizationfacts.Binding `json:"binding"`
}

type TransferObjectReadScope struct {
	ConfigDigest   string               `json:"config_digest"`
	ObjectSource   TransferObjectSource `json:"object_source"`
	TargetEngineID uint                 `json:"target_engine_id"`
}

func (s *TransferObjectSource) UnmarshalJSON(raw []byte) error {
	fields, err := jsonmap.DecodeObjectStrict(raw, MaxExecutionConfigBytes, 64)
	if err != nil || len(fields) != 2 || fields["engine_version"] == nil || fields["binding"] == nil {
		return errTransferObjectScope
	}
	type wire TransferObjectSource
	var decoded wire
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*s = TransferObjectSource(decoded)
	return nil
}

func (s *TransferObjectReadScope) UnmarshalJSON(raw []byte) error {
	fields, err := jsonmap.DecodeObjectStrict(raw, MaxExecutionConfigBytes, 64)
	if err != nil || len(fields) != 3 || fields["config_digest"] == nil || fields["object_source"] == nil || fields["target_engine_id"] == nil {
		return errTransferObjectScope
	}
	type wire TransferObjectReadScope
	var decoded wire
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*s = TransferObjectReadScope(decoded)
	return nil
}

func (s TransferObjectSource) Validate() error {
	p := s.Binding.NativeTarget
	if !positiveFileSourceVersion(s.EngineVersion) || s.Binding.Validate() != nil || s.Binding.FileComposition != nil ||
		s.Binding.Scope != nil || s.Binding.MetadataTarget != nil || len(p.Segments) < 3 || p.EngineID > uint(1<<63-1) ||
		p.Segments[0].Term != plugin.EngineCatalogTermService || p.Segments[1].Term != plugin.EngineCatalogTermBucket ||
		p.Segments[len(p.Segments)-1].Term != plugin.EngineCatalogTermObject {
		return errTransferObjectScope
	}
	return nil
}

func NewTransferObjectReadScope(raw json.RawMessage) (*TransferObjectReadScope, error) {
	config, digest, err := executionConfigObject(raw)
	if err != nil {
		return nil, errTransferObjectScope
	}
	encoded, err := json.Marshal(config["object_source"])
	if err != nil {
		return nil, errTransferObjectScope
	}
	s := &TransferObjectReadScope{ConfigDigest: digest}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&s.ObjectSource) != nil || s.ObjectSource.Validate() != nil {
		return nil, errTransferObjectScope
	}
	loc, dst, err := TransferObjectImportLocators(config)
	if err != nil {
		return nil, err
	}
	p := plugin.ObjectItemPath(loc.EngineID, loc.Path[0], strings.Join(loc.Path[1:], "/"))
	if !reflect.DeepEqual(p, s.ObjectSource.Binding.NativeTarget) {
		return nil, errTransferObjectScope
	}
	s.TargetEngineID = dst.EngineID
	return s, s.Validate()
}

// TransferObjectImportLocators validates the definition shared by preflight
// and execution authorization. It reads no engine or user authorization facts.
func TransferObjectImportLocators(config map[string]any) (sourceLocator, targetParent *resourcetree.ResourceLocator, err error) {
	runtime, _ := config["runtime"].(map[string]any)
	load, _ := config["load"].(map[string]any)
	source, _ := config["source"].(map[string]any)
	target, _ := config["target"].(map[string]any)
	for key := range source {
		switch key {
		case "locator", "data_type", "representation", "format", "options":
		default:
			return nil, nil, errTransferObjectScope
		}
	}
	if options, present := source["options"]; present {
		if _, ok := options.(map[string]any); !ok {
			return nil, nil, errTransferObjectScope
		}
	}
	if runtime["boundary"] != "bounded" || load["mode"] != "snapshot" ||
		source["data_type"] != "table" || source["representation"] != "encoded" || source["format"] != "parquet" ||
		target["data_type"] != "table" || target["representation"] != "native" || target["locator"] != nil ||
		config["filegdb_source"] != nil || config["workflow_result"] != nil {
		return nil, nil, errTransferObjectScope
	}
	uri, _ := source["locator"].(string)
	loc, err := resourcetree.ParseURI(uri)
	if err != nil || loc.ToURI() != uri || loc.Type != resourcetree.TypeObject || (loc.ItemID == nil || *loc.ItemID == 0) || len(loc.Path) < 2 {
		return nil, nil, errTransferObjectScope
	}
	parent, _ := target["parent_locator"].(string)
	dst, err := resourcetree.ParseURI(parent)
	name, _ := target["name"].(string)
	if err != nil || dst.ToURI() != parent || dst.Type != resourcetree.TypeSchema || len(dst.Path) != 1 || dst.EngineID == loc.EngineID ||
		name == "" || strings.ContainsAny(name, "/\\") || dst.EngineID > uint(1<<63-1) {
		return nil, nil, errTransferObjectScope
	}
	return loc, dst, nil
}

func (s *TransferObjectReadScope) Validate() error {
	if s == nil || !authorizationfacts.ValidItemFingerprint(s.ConfigDigest) || s.ObjectSource.Validate() != nil ||
		s.TargetEngineID == 0 || s.TargetEngineID > uint(1<<63-1) || s.TargetEngineID == s.ObjectSource.Binding.NativeTarget.EngineID {
		return errTransferObjectScope
	}
	return nil
}

func (s *TransferObjectReadScope) Clone() *TransferObjectReadScope {
	if s == nil {
		return nil
	}
	copy := *s
	copy.ObjectSource.Binding.NativeTarget.Segments = append([]plugin.EngineCatalogSegment(nil), s.ObjectSource.Binding.NativeTarget.Segments...)
	return &copy
}
