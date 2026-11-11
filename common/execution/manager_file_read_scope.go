package execution

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"reflect"
	"strconv"
)

const ManagerRasterCOGConfigVersion = "raster-cog-file-config/v1"

// ManagerFileReadScope is a frozen boundary, not a credential or Allow.
// System independently proves the pending config, engine and source rights.
type ManagerFileReadScope struct {
	ConfigDigest     string                                     `json:"config_digest"`
	OwnerTaskID      string                                     `json:"owner_task_id"`
	OwnerTaskVersion string                                     `json:"owner_task_version"`
	EngineVersion    string                                     `json:"engine_version"`
	FileComposition  authorizationfacts.FileCompositionBoundary `json:"file_composition"`
}

var errManagerFileScope = errors.New("invalid frozen file source scope")

// NewManagerFileReadScope binds every raw conversion value and verifies the
// shared wrapper; private conversion options remain the Manager owner's facts.
func NewManagerFileReadScope(raw json.RawMessage, taskID, taskVersion, engineVersion string,
	composition *authorizationfacts.FileCompositionBoundary) (*ManagerFileReadScope, error) {
	if composition == nil {
		return nil, errManagerFileScope
	}
	config, digest, err := executionConfigObject(raw)
	version := ManagerModel3DConfigVersion
	objects := []string{"source", "result", "options"}
	if composition.Format == "envi" || composition.Format == "tiff" {
		version = ManagerRasterCOGConfigVersion
		objects = []string{"target", "raster", "cog", "result"}
	}
	if err != nil || config["config_version"] != version || config["source_engine_version"] != engineVersion {
		return nil, errManagerFileScope
	}
	owner, ok := config["owner_task"].(map[string]any)
	if !ok || len(owner) != 2 || owner["id"] != taskID || owner["version"] != taskVersion {
		return nil, errManagerFileScope
	}
	for _, name := range objects {
		if value, ok := config[name].(map[string]any); !ok || value == nil {
			return nil, errManagerFileScope
		}
	}
	encoded, err := json.Marshal(config["source_composition"])
	var declared authorizationfacts.FileCompositionBoundary
	if err != nil || json.Unmarshal(encoded, &declared) != nil || !reflect.DeepEqual(&declared, composition) {
		return nil, errManagerFileScope
	}
	scope := &ManagerFileReadScope{ConfigDigest: digest, OwnerTaskID: taskID, OwnerTaskVersion: taskVersion,
		EngineVersion: engineVersion, FileComposition: *composition.Clone()}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return scope, nil
}

func (s *ManagerFileReadScope) Validate() error {
	if s == nil || !authorizationfacts.ValidItemFingerprint(s.ConfigDigest) || !positiveFileSourceVersion(s.OwnerTaskID) ||
		!positiveFileSourceVersion(s.OwnerTaskVersion) || !positiveFileSourceVersion(s.EngineVersion) ||
		s.Purpose() == "" {
		return errManagerFileScope
	}
	primary, err := s.FileComposition.PrimaryTarget()
	if err != nil || (primary.Segments[len(primary.Segments)-1].Term != plugin.EngineCatalogTermFile &&
		primary.Segments[len(primary.Segments)-1].Term != plugin.EngineCatalogTermObject) ||
		((s.FileComposition.Format == "ifc" || s.FileComposition.Format == "stl" || s.FileComposition.Format == "fbx") && len(s.FileComposition.Refs) != 1) {
		return errManagerFileScope
	}
	if s.FileComposition.Format == "fbx" && s.FileComposition.Refs[0].Role != "model" {
		return errManagerFileScope
	}
	return nil
}

// Purpose follows the closed admitted format identity, never a caller-supplied
// purpose string or the number of files in the composition.
func (s *ManagerFileReadScope) Purpose() string {
	if s == nil {
		return ""
	}
	if s.FileComposition.Format == "envi" || s.FileComposition.Format == "tiff" {
		return TaskTypeRasterCOGGeneration
	}
	if ManagerModel3DConversionFormat(s.FileComposition.Format) {
		return TaskTypeModel3DGLBGeneration
	}
	return ""
}

func (s *ManagerFileReadScope) Clone() *ManagerFileReadScope {
	if s == nil {
		return nil
	}
	copy := *s
	copy.FileComposition = *s.FileComposition.Clone()
	return &copy
}

func positiveFileSourceVersion(value string) bool {
	id, err := strconv.ParseInt(value, 10, 64)
	return err == nil && id > 0 && strconv.FormatInt(id, 10) == value
}

func (s *ManagerFileReadScope) UnmarshalJSON(encoded []byte) error {
	fields, _, err := executionConfigObject(encoded)
	if err != nil || len(fields) != 5 {
		return errManagerFileScope
	}
	for _, key := range []string{"config_digest", "owner_task_id", "owner_task_version", "engine_version", "file_composition"} {
		if fields[key] == nil {
			return errManagerFileScope
		}
	}
	type wire ManagerFileReadScope
	var decoded wire
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&decoded) != nil || (*ManagerFileReadScope)(&decoded).Validate() != nil {
		return errManagerFileScope
	}
	*s = ManagerFileReadScope(decoded)
	return nil
}
