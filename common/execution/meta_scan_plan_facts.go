package execution

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/jsonmap"
	"github.com/google/uuid"
)

const MetaScanPlanFactsSchemaV1 = "meta.scan_plan_facts/v1"

// MetaScanPlanFacts is an immutable plan's owner projection, never an Allow.
// System independently checks execution identity, current qualification and engine.
type MetaScanPlanFacts struct {
	SchemaVersion        string                                     `json:"schema_version"`
	ExecutionID          string                                     `json:"execution_id"`
	TaskID               *string                                    `json:"task_id"`
	TaskDefinitionDigest *string                                    `json:"task_definition_digest"`
	ExecutionPlanDigest  string                                     `json:"execution_plan_digest"`
	EngineID             uint                                       `json:"engine_id,string" swaggertype:"string"`
	EngineVersion        string                                     `json:"engine_version"`
	ScanDepth            string                                     `json:"scan_depth" enums:"basic,deep"`
	SourceScope          *MetaScanSourceScope                       `json:"source_scope"`
	ReadOptions          MetaScanReadOptions                        `json:"read_options"`
	BasisState           authorizationfacts.AuthorizationBasisState `json:"basis_state" enums:"complete,incomplete,needs_refresh,unsupported"`
}

// MetaScanSourceScope describes anchors proven by Meta, not a caller's selector.
// Anchors sort by json.Marshal(path) bytes; names are preserved without trimming.
type MetaScanSourceScope struct {
	Mode    string                     `json:"mode" enums:"exact,full"`
	Anchors []plugin.EngineCatalogPath `json:"anchors"`
}

type MetaScanReadOptions struct {
	Encoding  string `json:"encoding,omitempty"`
	HasHeader *bool  `json:"has_header,omitempty"`
}

// ComputeMetaScanExecutionPlanDigest covers the entire raw execution object.
func ComputeMetaScanExecutionPlanDigest(raw json.RawMessage) (string, error) {
	_, digest, err := executionConfigObject(raw)
	return digest, err
}

// ComputeMetaScanTaskDefinitionDigest covers an explicit functional definition
// built by the owner, excluding mutable runtime summaries and display names.
func ComputeMetaScanTaskDefinitionDigest(raw json.RawMessage) (string, error) {
	_, digest, err := executionConfigObject(raw)
	return digest, err
}

func metaScanPositiveDecimal(value string) bool {
	n, err := strconv.ParseUint(value, 10, 63)
	return err == nil && n > 0 && strconv.FormatUint(n, 10) == value
}

func metaScanText(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n")
}

func (f MetaScanPlanFacts) Validate() error {
	id, err := uuid.Parse(f.ExecutionID)
	if err != nil || id == uuid.Nil || id.String() != f.ExecutionID || f.SchemaVersion != MetaScanPlanFactsSchemaV1 ||
		f.EngineID == 0 || uint64(f.EngineID) > math.MaxInt64 || !metaScanPositiveDecimal(f.EngineVersion) ||
		!authorizationfacts.ValidItemFingerprint(f.ExecutionPlanDigest) || (f.ScanDepth != "basic" && f.ScanDepth != "deep") {
		return errors.New("invalid Meta scan plan identity")
	}
	if f.TaskID == nil {
		if f.TaskDefinitionDigest != nil {
			return errors.New("ad-hoc Meta scan cannot declare a task definition")
		}
	} else if !metaScanPositiveDecimal(*f.TaskID) || f.TaskDefinitionDigest == nil || !authorizationfacts.ValidItemFingerprint(*f.TaskDefinitionDigest) {
		return errors.New("invalid Meta scan task definition identity")
	}
	if f.ReadOptions.Encoding != "" && !metaScanText(f.ReadOptions.Encoding, 64) {
		return errors.New("invalid Meta scan read options")
	}
	switch f.BasisState {
	case authorizationfacts.AuthorizationBasisComplete:
		if err := f.SourceScope.Validate(f.EngineID); err != nil {
			return err
		}
	case authorizationfacts.AuthorizationBasisIncomplete, authorizationfacts.AuthorizationBasisNeedsRefresh, authorizationfacts.AuthorizationBasisUnsupported:
		if f.SourceScope != nil {
			return errors.New("unresolved Meta scan facts cannot declare a scope")
		}
	default:
		return errors.New("invalid Meta scan basis state")
	}
	raw, err := json.Marshal(f)
	if err != nil || len(raw) > MaxExecutionConfigBytes {
		return errors.New("Meta scan facts exceed size boundary")
	}
	return nil
}

// Validate checks only value structure. It does not prove existence, catalog
// model compatibility, ownership, composition completeness or access rights.
func (s *MetaScanSourceScope) Validate(engineID uint) error {
	invalid := errors.New("invalid Meta scan source scope")
	if s == nil || (s.Mode != "exact" && s.Mode != "full") || len(s.Anchors) == 0 || len(s.Anchors) > 200 || engineID == 0 {
		return invalid
	}
	var previous []byte
	for _, path := range s.Anchors {
		if path.EngineID != engineID || path.Version != plugin.EngineCatalogPathVersion || len(path.Segments) == 0 || len(path.Segments) > 64 {
			return invalid
		}
		root := path.Segments[0]
		if !plugin.IsEngineCatalogRootSegment(root) || root.Name != "" || root.Kind != root.Term {
			return invalid
		}
		for _, segment := range path.Segments[1:] {
			if plugin.IsEngineCatalogRootSegment(segment) || !metaScanText(segment.Term, 64) || strings.TrimSpace(segment.Term) != segment.Term || !metaScanText(segment.Kind, 64) || strings.TrimSpace(segment.Kind) != segment.Kind || !metaScanText(segment.Name, 4096) {
				return invalid
			}
		}
		if s.Mode == "exact" {
			if len(path.Segments) < 2 {
				return invalid
			}
			last := path.Segments[len(path.Segments)-1]
			if last.Kind == plugin.EngineCatalogKindNamespace || last.Kind == plugin.EngineCatalogKindDirectory || last.Kind == plugin.EngineCatalogKindPrefix || last.Kind == plugin.EngineCatalogKindBucket || last.Term == plugin.EngineCatalogTermDirectory || last.Term == plugin.EngineCatalogTermPrefix || last.Term == plugin.EngineCatalogTermBucket || last.Term == plugin.EngineCatalogTermSchema || last.Term == plugin.EngineCatalogTermDatabase {
				return invalid
			}
		}
		key, err := json.Marshal(path)
		if err != nil || (previous != nil && bytes.Compare(previous, key) >= 0) {
			return invalid
		}
		previous = key
	}
	return nil
}

func (f *MetaScanPlanFacts) Clone() *MetaScanPlanFacts {
	if f == nil {
		return nil
	}
	copy := *f
	if f.TaskID != nil {
		value := *f.TaskID
		copy.TaskID = &value
	}
	if f.TaskDefinitionDigest != nil {
		value := *f.TaskDefinitionDigest
		copy.TaskDefinitionDigest = &value
	}
	if f.ReadOptions.HasHeader != nil {
		value := *f.ReadOptions.HasHeader
		copy.ReadOptions.HasHeader = &value
	}
	if f.SourceScope != nil {
		scope := *f.SourceScope
		scope.Anchors = append([]plugin.EngineCatalogPath(nil), scope.Anchors...)
		for i := range scope.Anchors {
			scope.Anchors[i].Segments = append([]plugin.EngineCatalogSegment(nil), scope.Anchors[i].Segments...)
		}
		copy.SourceScope = &scope
	}
	return &copy
}

func metaScanClosedObject(value any, required, optional []string, nullable map[string]bool) bool {
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, key := range required {
		allowed[key] = true
		if _, exists := object[key]; !exists {
			return false
		}
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key, value := range object {
		if !allowed[key] || (value == nil && !nullable[key]) {
			return false
		}
	}
	return true
}

func (f *MetaScanPlanFacts) UnmarshalJSON(raw []byte) error {
	invalid := errors.New("invalid strict Meta scan plan facts")
	object, err := jsonmap.DecodeObjectStrict(raw, MaxExecutionConfigBytes, 64)
	if err != nil || !metaScanClosedObject(object, []string{"schema_version", "execution_id", "task_id", "task_definition_digest", "execution_plan_digest", "engine_id", "engine_version", "scan_depth", "source_scope", "read_options", "basis_state"}, nil, map[string]bool{"task_id": true, "task_definition_digest": true, "source_scope": true}) ||
		!metaScanClosedObject(object["read_options"], nil, []string{"encoding", "has_header"}, nil) {
		return invalid
	}
	if scope := object["source_scope"]; scope != nil {
		if !metaScanClosedObject(scope, []string{"mode", "anchors"}, nil, nil) {
			return invalid
		}
		anchors, ok := scope.(map[string]any)["anchors"].([]any)
		if !ok {
			return invalid
		}
		for _, anchor := range anchors {
			if !metaScanClosedObject(anchor, []string{"version", "engine_id", "segments"}, nil, nil) {
				return invalid
			}
			segments, ok := anchor.(map[string]any)["segments"].([]any)
			if !ok {
				return invalid
			}
			for _, segment := range segments {
				if !metaScanClosedObject(segment, []string{"term", "kind", "name"}, nil, nil) {
					return invalid
				}
			}
		}
	}
	type plain MetaScanPlanFacts
	var result plain
	if json.Unmarshal(raw, &result) != nil || object["engine_id"] != strconv.FormatUint(uint64(result.EngineID), 10) || MetaScanPlanFacts(result).Validate() != nil {
		return invalid
	}
	*f = MetaScanPlanFacts(result)
	return nil
}
