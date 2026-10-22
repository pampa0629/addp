package execution

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
)

const ManagerModel3DConfigVersion = "model3d-glb-file-config/v1"

// ManagerModel3DSourceEngineType restricts the admitted source adapters.
func ManagerModel3DSourceEngineType(engineType string) bool {
	return engineType == "ftp" || engineType == "nfs" || engineType == "minio"
}

// ManagerModel3DSourceFormat is the single admission rule for conversion routes.
func ManagerModel3DSourceFormat(engineType, format string) bool {
	switch engineType {
	case "ftp", "nfs":
		return format == "ifc" || format == "3ds" || format == "dae"
	case "minio":
		return format == "ifc" || format == "3ds"
	default:
		return false
	}
}

// ManagerModel3DSourceComposition also proves the native catalog shape. Engine
// identity alone must never let a file scope authorize an object or vice versa.
func ManagerModel3DSourceComposition(engineType string, composition *authorizationfacts.FileCompositionBoundary) bool {
	if composition == nil || !ManagerModel3DSourceFormat(engineType, composition.Format) {
		return false
	}
	primary, err := composition.PrimaryTarget()
	if err != nil {
		return false
	}
	term := primary.Segments[len(primary.Segments)-1].Term
	return (engineType == "minio" && term == plugin.EngineCatalogTermObject) ||
		((engineType == "ftp" || engineType == "nfs") && term == plugin.EngineCatalogTermFile)
}

// ManagerModel3DReadScope is a frozen boundary, not a credential or Allow.
// System independently proves the pending config, engine and source rights.
type ManagerModel3DReadScope struct {
	ConfigDigest     string                                     `json:"config_digest"`
	OwnerTaskID      string                                     `json:"owner_task_id"`
	OwnerTaskVersion string                                     `json:"owner_task_version"`
	EngineVersion    string                                     `json:"engine_version"`
	FileComposition  authorizationfacts.FileCompositionBoundary `json:"file_composition"`
}

var errManagerModel3DScope = errors.New("invalid frozen file model3d source scope")

// NewManagerModel3DReadScope binds every raw conversion value and verifies the
// shared wrapper; private conversion options remain the Manager owner's facts.
func NewManagerModel3DReadScope(raw json.RawMessage, taskID, taskVersion, engineVersion string,
	composition *authorizationfacts.FileCompositionBoundary) (*ManagerModel3DReadScope, error) {
	if composition == nil {
		return nil, errManagerModel3DScope
	}
	config, digest, err := executionConfigObject(raw)
	if err != nil || config["config_version"] != ManagerModel3DConfigVersion || config["source_engine_version"] != engineVersion {
		return nil, errManagerModel3DScope
	}
	owner, ok := config["owner_task"].(map[string]any)
	if !ok || len(owner) != 2 || owner["id"] != taskID || owner["version"] != taskVersion {
		return nil, errManagerModel3DScope
	}
	for _, name := range []string{"source", "result", "options"} {
		if value, ok := config[name].(map[string]any); !ok || value == nil {
			return nil, errManagerModel3DScope
		}
	}
	encoded, err := json.Marshal(config["source_composition"])
	var declared authorizationfacts.FileCompositionBoundary
	if err != nil || json.Unmarshal(encoded, &declared) != nil || !reflect.DeepEqual(&declared, composition) {
		return nil, errManagerModel3DScope
	}
	scope := &ManagerModel3DReadScope{ConfigDigest: digest, OwnerTaskID: taskID, OwnerTaskVersion: taskVersion,
		EngineVersion: engineVersion, FileComposition: *composition.Clone()}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return scope, nil
}

func (s *ManagerModel3DReadScope) Validate() error {
	if s == nil || !authorizationfacts.ValidItemFingerprint(s.ConfigDigest) || !positiveModel3DVersion(s.OwnerTaskID) ||
		!positiveModel3DVersion(s.OwnerTaskVersion) || !positiveModel3DVersion(s.EngineVersion) ||
		(s.FileComposition.Format != "ifc" && s.FileComposition.Format != "3ds" && s.FileComposition.Format != "dae") {
		return errManagerModel3DScope
	}
	primary, err := s.FileComposition.PrimaryTarget()
	if err != nil || (primary.Segments[len(primary.Segments)-1].Term != plugin.EngineCatalogTermFile &&
		(primary.Segments[len(primary.Segments)-1].Term != plugin.EngineCatalogTermObject || s.FileComposition.Format == "dae")) ||
		(s.FileComposition.Format == "ifc" && len(s.FileComposition.Refs) != 1) {
		return errManagerModel3DScope
	}
	return nil
}

func (s *ManagerModel3DReadScope) Clone() *ManagerModel3DReadScope {
	if s == nil {
		return nil
	}
	copy := *s
	copy.FileComposition = *s.FileComposition.Clone()
	return &copy
}

func positiveModel3DVersion(value string) bool {
	id, err := strconv.ParseInt(value, 10, 64)
	return err == nil && id > 0 && strconv.FormatInt(id, 10) == value
}

func (s *ManagerModel3DReadScope) UnmarshalJSON(encoded []byte) error {
	fields, _, err := executionConfigObject(encoded)
	if err != nil || len(fields) != 5 {
		return errManagerModel3DScope
	}
	for _, key := range []string{"config_digest", "owner_task_id", "owner_task_version", "engine_version", "file_composition"} {
		if fields[key] == nil {
			return errManagerModel3DScope
		}
	}
	type wire ManagerModel3DReadScope
	var decoded wire
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&decoded) != nil || (*ManagerModel3DReadScope)(&decoded).Validate() != nil {
		return errManagerModel3DScope
	}
	*s = ManagerModel3DReadScope(decoded)
	return nil
}
