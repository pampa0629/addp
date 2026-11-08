package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
)

const (
	DevelopFileIntentSchema     = "develop.geopackage-file-intent/v1"
	DevelopBoundFileScopeSchema = "develop.geopackage-bound-scope/v1"
)

var errDevelopFileScope = errors.New("invalid Develop file execution boundary")

// NodeID identifies a DAG node, not a MetaNode or an authorization identity.
type DevelopFileSourceIntent struct {
	NodeID string                   `json:"node_id"`
	Target plugin.EngineCatalogPath `json:"target"`
}

type DevelopFileTargetIntent struct {
	NodeID string                   `json:"node_id"`
	Target plugin.EngineCatalogPath `json:"target"`
	Mode   string                   `json:"mode"`
}

// DevelopFileIntent freezes the owner's declaration. It contains no guessed
// Meta fingerprint/hash and grants no access. The owner proves DAG coverage.
type DevelopFileIntent struct {
	SchemaVersion   string                    `json:"schema_version"`
	ConfigDigest    string                    `json:"config_digest"`
	ExpectedAttempt int                       `json:"expected_attempt"`
	TimeoutSeconds  int                       `json:"timeout_seconds"`
	Sources         []DevelopFileSourceIntent `json:"sources"`
	Target          *DevelopFileTargetIntent  `json:"target,omitempty"`
}

func NewDevelopFileIntent(raw json.RawMessage, expectedAttempt, timeoutSeconds int, sources []DevelopFileSourceIntent, target *DevelopFileTargetIntent) (*DevelopFileIntent, error) {
	if len(sources) == 0 {
		return nil, errDevelopFileScope
	}
	_, digest, err := executionConfigObject(raw)
	if err != nil {
		return nil, errDevelopFileScope
	}
	value := (&DevelopFileIntent{SchemaVersion: DevelopFileIntentSchema, ConfigDigest: digest,
		ExpectedAttempt: expectedAttempt, TimeoutSeconds: timeoutSeconds, Sources: sources, Target: target}).Clone()
	sort.Slice(value.Sources, func(i, j int) bool { return value.Sources[i].NodeID < value.Sources[j].NodeID })
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *DevelopFileIntent) Validate() error {
	if s == nil || s.SchemaVersion != DevelopFileIntentSchema || !developFileDigest(s.ConfigDigest) ||
		!validDevelopFileBudget(s.ExpectedAttempt, s.TimeoutSeconds) || len(s.Sources) == 0 {
		return errDevelopFileScope
	}
	for i, source := range s.Sources {
		if !developFileNodeID(source.NodeID) || !developFileLeaf(source.Target) || (i > 0 && s.Sources[i-1].NodeID >= source.NodeID) {
			return errDevelopFileScope
		}
		if s.Target != nil && source.NodeID == s.Target.NodeID {
			return errDevelopFileScope
		}
	}
	if s.Target != nil && (!developFileNodeID(s.Target.NodeID) || s.Target.Mode != "fail" || !developFileLeaf(s.Target.Target)) {
		return errDevelopFileScope
	}
	return boundedDevelopFileValue(s)
}

func (s *DevelopFileIntent) Digest() (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	return developFileValueDigest(s)
}

func (s *DevelopFileIntent) Clone() *DevelopFileIntent {
	if s == nil {
		return nil
	}
	result := *s
	result.Sources = append([]DevelopFileSourceIntent(nil), s.Sources...)
	for i := range result.Sources {
		result.Sources[i].Target = cloneDevelopFilePath(result.Sources[i].Target)
	}
	if s.Target != nil {
		target := *s.Target
		target.Target = cloneDevelopFilePath(target.Target)
		result.Target = &target
	}
	return &result
}

// References are values supplied after System's independent Meta/IAM checks.
// Syntax validation or construction alone does not prove admission or an Allow.
type DevelopFileSourceReference struct {
	Target          plugin.EngineCatalogPath `json:"target"`
	ItemFingerprint string                   `json:"item_fingerprint"`
	BoundaryHash    string                   `json:"boundary_hash"`
	EngineVersion   string                   `json:"engine_version"`
}

type DevelopBoundFileTarget struct {
	Target        plugin.EngineCatalogPath `json:"target"`
	Mode          string                   `json:"mode"`
	EngineVersion string                   `json:"engine_version"`
}

// DevelopBoundFileScope is the value sealed with an EA by System. Its digest
// is not a credential. System retains the authoritative full composition.
type DevelopBoundFileScope struct {
	SchemaVersion   string                       `json:"schema_version"`
	IntentDigest    string                       `json:"intent_digest"`
	ExpectedAttempt int                          `json:"expected_attempt"`
	TimeoutSeconds  int                          `json:"timeout_seconds"`
	Sources         []DevelopFileSourceReference `json:"sources"`
	Target          *DevelopBoundFileTarget      `json:"target,omitempty"`
}

func NewDevelopBoundFileScope(intent DevelopFileIntent, sources []DevelopFileSourceReference, targetEngineVersion string) (*DevelopBoundFileScope, error) {
	digest, err := intent.Digest()
	if err != nil || len(sources) == 0 {
		return nil, errDevelopFileScope
	}
	refs := make(map[string]DevelopFileSourceReference, len(sources))
	paths := make([]plugin.EngineCatalogPath, 0, len(sources))
	for _, source := range sources {
		if !validDevelopFileSourceReference(source) {
			return nil, errDevelopFileScope
		}
		key := developFilePathKey(source.Target)
		if previous, exists := refs[key]; exists && !reflect.DeepEqual(previous, source) {
			return nil, errDevelopFileScope
		}
		refs[key] = source
		paths = append(paths, source.Target)
	}
	canonical, err := plugin.NewQueryReadSet(paths...)
	if err != nil {
		return nil, errDevelopFileScope
	}
	result := &DevelopBoundFileScope{SchemaVersion: DevelopBoundFileScopeSchema, IntentDigest: digest,
		ExpectedAttempt: intent.ExpectedAttempt, TimeoutSeconds: intent.TimeoutSeconds}
	for _, path := range canonical.Paths {
		ref := refs[developFilePathKey(path)]
		ref.Target = cloneDevelopFilePath(path)
		result.Sources = append(result.Sources, ref)
	}
	if intent.Target == nil {
		if targetEngineVersion != "" {
			return nil, errDevelopFileScope
		}
	} else {
		result.Target = &DevelopBoundFileTarget{Target: cloneDevelopFilePath(intent.Target.Target), Mode: intent.Target.Mode, EngineVersion: targetEngineVersion}
	}
	if err := result.ValidateAgainstIntent(intent); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *DevelopBoundFileScope) Validate() error {
	if s == nil || s.SchemaVersion != DevelopBoundFileScopeSchema || !developFileDigest(s.IntentDigest) ||
		!validDevelopFileBudget(s.ExpectedAttempt, s.TimeoutSeconds) || len(s.Sources) == 0 {
		return errDevelopFileScope
	}
	paths := make([]plugin.EngineCatalogPath, 0, len(s.Sources))
	seen := make(map[string]bool, len(s.Sources))
	for _, source := range s.Sources {
		if !validDevelopFileSourceReference(source) || seen[source.ItemFingerprint] {
			return errDevelopFileScope
		}
		seen[source.ItemFingerprint] = true
		paths = append(paths, source.Target)
	}
	canonical, err := plugin.NewQueryReadSet(paths...)
	if err != nil || !reflect.DeepEqual(canonical.Paths, paths) {
		return errDevelopFileScope
	}
	if s.Target != nil && (!developFileLeaf(s.Target.Target) || s.Target.Mode != "fail" || !developFileVersion(s.Target.EngineVersion)) {
		return errDevelopFileScope
	}
	return boundedDevelopFileValue(s)
}

func (s *DevelopBoundFileScope) ValidateAgainstIntent(intent DevelopFileIntent) error {
	digest, err := intent.Digest()
	if err != nil || s.Validate() != nil || s.IntentDigest != digest || s.ExpectedAttempt != intent.ExpectedAttempt || s.TimeoutSeconds != intent.TimeoutSeconds || (s.Target == nil) != (intent.Target == nil) {
		return errDevelopFileScope
	}
	paths := make([]plugin.EngineCatalogPath, 0, len(intent.Sources))
	for _, source := range intent.Sources {
		paths = append(paths, source.Target)
	}
	canonical, err := plugin.NewQueryReadSet(paths...)
	if err != nil || len(canonical.Paths) != len(s.Sources) {
		return errDevelopFileScope
	}
	for i, path := range canonical.Paths {
		if !reflect.DeepEqual(path, s.Sources[i].Target) {
			return errDevelopFileScope
		}
	}
	if s.Target != nil && (!reflect.DeepEqual(s.Target.Target, intent.Target.Target) || s.Target.Mode != intent.Target.Mode) {
		return errDevelopFileScope
	}
	return nil
}

func (s *DevelopBoundFileScope) Digest() (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	return developFileValueDigest(s)
}

// Deadline uses the actual database claim time, never pending creation or now.
func (s *DevelopBoundFileScope) Deadline(startedAt time.Time) (time.Time, error) {
	if s.Validate() != nil || startedAt.IsZero() {
		return time.Time{}, errDevelopFileScope
	}
	deadline := startedAt.UTC().Add(time.Duration(s.TimeoutSeconds) * time.Second)
	if deadline.Year() < 1 || deadline.Year() > 9999 {
		return time.Time{}, errDevelopFileScope
	}
	return deadline, nil
}

func (s *DevelopBoundFileScope) Clone() *DevelopBoundFileScope {
	if s == nil {
		return nil
	}
	result := *s
	result.Sources = append([]DevelopFileSourceReference(nil), s.Sources...)
	for i := range result.Sources {
		result.Sources[i].Target = cloneDevelopFilePath(result.Sources[i].Target)
	}
	if s.Target != nil {
		target := *s.Target
		target.Target = cloneDevelopFilePath(target.Target)
		result.Target = &target
	}
	return &result
}

func validDevelopFileSourceReference(source DevelopFileSourceReference) bool {
	return developFileLeaf(source.Target) && authorizationfacts.ValidItemFingerprint(source.ItemFingerprint) &&
		authorizationfacts.ValidItemFingerprint(source.BoundaryHash) && developFileVersion(source.EngineVersion)
}

func validDevelopFileBudget(attempt, seconds int) bool {
	return attempt > 0 && seconds > 0 && int64(seconds) <= math.MaxInt64/int64(time.Second)
}

func developFileNodeID(id string) bool { return utf8.ValidString(id) && strings.TrimSpace(id) != "" }

func developFileVersion(version string) bool {
	n, err := strconv.ParseInt(version, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == version
}

func developFileDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(value) == value
}

func developFileLeaf(path plugin.EngineCatalogPath) bool {
	if path.EngineID == 0 || uint64(path.EngineID) > math.MaxInt64 || path.Version != plugin.EngineCatalogPathVersion || len(path.Segments) < 2 ||
		!reflect.DeepEqual(path, plugin.FileItemPath(path.EngineID, path.StringPath())) {
		return false
	}
	for _, part := range path.Segments[1:] {
		if part.Name == "" || part.Name == "." || part.Name == ".." || strings.ContainsAny(part.Name, "/\x00") || !utf8.ValidString(part.Name) {
			return false
		}
	}
	return true
}

func cloneDevelopFilePath(path plugin.EngineCatalogPath) plugin.EngineCatalogPath {
	path.Segments = append([]plugin.EngineCatalogSegment(nil), path.Segments...)
	return path
}

func developFilePathKey(path plugin.EngineCatalogPath) string {
	encoded, _ := json.Marshal(path)
	return string(encoded)
}

func boundedDevelopFileValue(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > MaxExecutionConfigBytes {
		return errDevelopFileScope
	}
	return nil
}

func developFileValueDigest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", errDevelopFileScope
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
