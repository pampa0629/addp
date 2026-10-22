package execution

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/jsonmap"
)

const MetaScanTaskFactsSchemaV1 = "meta.scan_task_facts/v1"

// MetaScanTaskFacts describes a current task before execution. It does not
// contain an Engine version: System owns that fact and freezes it on approval.
// The first supported selector is an explicitly declared entire Engine. Other
// selectors must not be projected as this scope, even if they cannot resolve.
type MetaScanTaskFacts struct {
	SchemaVersion        string              `json:"schema_version"`
	TaskID               string              `json:"task_id"`
	TaskDefinitionDigest string              `json:"task_definition_digest"`
	EngineID             uint                `json:"engine_id,string" swaggertype:"string"`
	ScopeKind            string              `json:"scope_kind" enums:"engine"`
	ScanDepth            string              `json:"scan_depth" enums:"basic,deep"`
	Force                bool                `json:"force"`
	ReadOptions          MetaScanReadOptions `json:"read_options"`
}

func (f MetaScanTaskFacts) Validate() error {
	if f.SchemaVersion != MetaScanTaskFactsSchemaV1 || !metaScanPositiveDecimal(f.TaskID) ||
		!authorizationfacts.ValidItemFingerprint(f.TaskDefinitionDigest) || f.EngineID == 0 || uint64(f.EngineID) > math.MaxInt64 ||
		f.ScopeKind != "engine" || (f.ScanDepth != "basic" && f.ScanDepth != "deep") ||
		(f.ReadOptions.Encoding != "" && !metaScanText(f.ReadOptions.Encoding, 64)) {
		return errors.New("invalid Meta scan task facts")
	}
	return nil
}

func (f *MetaScanTaskFacts) UnmarshalJSON(raw []byte) error {
	invalid := errors.New("invalid strict Meta scan task facts")
	object, err := jsonmap.DecodeObjectStrict(raw, MaxExecutionConfigBytes, 64)
	if err != nil || !metaScanClosedObject(object, []string{"schema_version", "task_id", "task_definition_digest", "engine_id", "scope_kind", "scan_depth", "force", "read_options"}, nil, nil) ||
		!metaScanClosedObject(object["read_options"], nil, []string{"encoding", "has_header"}, nil) {
		return invalid
	}
	type plain MetaScanTaskFacts
	var result plain
	if json.Unmarshal(raw, &result) != nil || object["engine_id"] != strconv.FormatUint(uint64(result.EngineID), 10) || MetaScanTaskFacts(result).Validate() != nil {
		return invalid
	}
	*f = MetaScanTaskFacts(result)
	return nil
}
