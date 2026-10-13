package execution

import (
	"bytes"
	"encoding/json"
)

const FailureDiagnosticSchemaVersion = "addp.execution-failure/v1"

// FailureDiagnostic contains only controlled vocabulary, never runtime text.
// Owners choose the reason at the failing boundary. The shared UI owns the
// corresponding localized explanation and action, without dynamic parameters.
type FailureDiagnostic struct {
	SchemaVersion string `json:"schema_version"`
	Stage         string `json:"stage"`
	Reason        string `json:"reason"`
}

var failureStages = map[string]bool{
	"validation": true, "authorization": true, "preparation": true,
	"execution": true, "result": true,
}

var failureReasons = map[string]bool{
	"configuration_missing": true, "configuration_invalid": true,
	"timeout_invalid": true, "result_limit_invalid": true,
	"query_template_invalid": true, "relation_binding_invalid": true,
	"authorization_invalid": true, "engine_unavailable": true, "engine_mismatch": true,
	"query_syntax_invalid": true, "column_not_found": true, "table_not_found": true,
	"type_mismatch": true, "permission_denied": true, "connection_failed": true,
	"timeout": true, "cancelled": true, "resource_exhausted": true,
	"query_failed": true, "result_invalid": true,
	"database_required": true,
}

func NewFailureDiagnostic(stage, reason string) *FailureDiagnostic {
	if !failureStages[stage] || !failureReasons[reason] {
		return nil
	}
	return &FailureDiagnostic{SchemaVersion: FailureDiagnosticSchemaVersion, Stage: stage, Reason: reason}
}

func projectFailureDiagnostic(value interface{}) *FailureDiagnostic {
	payload, err := json.Marshal(value)
	if err != nil || len(payload) > 512 {
		return nil
	}
	var diagnostic FailureDiagnostic
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&diagnostic) != nil || diagnostic.SchemaVersion != FailureDiagnosticSchemaVersion {
		return nil
	}
	return NewFailureDiagnostic(diagnostic.Stage, diagnostic.Reason)
}
