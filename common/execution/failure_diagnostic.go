package execution

import (
	"bytes"
	"encoding/json"

	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
)

const FailureDiagnosticSchemaVersion = "addp.execution-failure/v3"

// FailureDiagnostic contains only controlled vocabulary, never runtime text.
// Owners choose the reason at the failing boundary. The shared UI owns the
// corresponding localized explanation and action. Optional SQL facts contain
// only provider-confirmed identifiers and an original-source location.
type FailureDiagnostic struct {
	SchemaVersion        string                    `json:"schema_version"`
	Stage                string                    `json:"stage"`
	Reason               string                    `json:"reason"`
	SQL                  *commonquery.ErrorContext `json:"sql,omitempty"`
	Write                *plugin.TableWriteField   `json:"write,omitempty"`
	TargetChangePossible *bool                     `json:"target_change_possible,omitempty"`
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
	"ambiguous_column": true, "function_or_operator_not_found": true, "division_by_zero": true,
	"timeout": true, "cancelled": true, "resource_exhausted": true,
	"query_failed": true, "result_invalid": true,
	"local_read_unsupported": true, "database_required": true, "data_protection_required": true,
	"json_invalid": true, "array_invalid": true, "bytes_invalid": true,
	"table_write_failed": true, "execution_failed": true,
	"target_exists": true,
}

func NewFailureDiagnostic(stage, reason string) *FailureDiagnostic {
	if !failureStages[stage] || !failureReasons[reason] {
		return nil
	}
	return &FailureDiagnostic{SchemaVersion: FailureDiagnosticSchemaVersion, Stage: stage, Reason: reason}
}

func WithSQLFailureContext(diagnostic *FailureDiagnostic, context *commonquery.ErrorContext) *FailureDiagnostic {
	if diagnostic == nil || context == nil {
		return diagnostic
	}
	copy := *diagnostic
	copy.SQL = context.Clone()
	copy.SQL.SourceDigest, copy.SQL.Offset = "", nil
	if !validSQLFailureContext(&copy) {
		return diagnostic
	}
	return &copy
}

func validSQLFailureContext(diagnostic *FailureDiagnostic) bool {
	context := diagnostic.SQL
	if context == nil {
		return true
	}
	if !context.Valid() {
		return false
	}
	allowed := map[string]string{"table_not_found": "table", "column_not_found": "column", "ambiguous_column": "column", "function_or_operator_not_found": "function|operator", "query_syntax_invalid": "", "type_mismatch": "", "division_by_zero": ""}
	kind, ok := allowed[diagnostic.Reason]
	if !ok {
		return false
	}
	if context.Object != nil && context.Object.Kind != kind && !(kind == "function|operator" && (context.Object.Kind == "function" || context.Object.Kind == "operator")) {
		return false
	}
	return true
}

func validWriteFailureContext(diagnostic *FailureDiagnostic) bool {
	if diagnostic.TargetChangePossible != nil && (!*diagnostic.TargetChangePossible ||
		(diagnostic.Stage != "preparation" && diagnostic.Stage != "execution" && diagnostic.Stage != "result")) {
		return false
	}
	return diagnostic.Write == nil || diagnostic.SQL == nil && diagnostic.Stage == "execution" &&
		diagnostic.Write.Valid(plugin.TableWriteErrorCode(diagnostic.Reason))
}

func projectFailureDiagnostic(value interface{}) *FailureDiagnostic {
	payload, err := json.Marshal(value)
	if err != nil || len(payload) > 2048 {
		return nil
	}
	var diagnostic FailureDiagnostic
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&diagnostic) != nil || diagnostic.SchemaVersion != FailureDiagnosticSchemaVersion {
		return nil
	}
	if NewFailureDiagnostic(diagnostic.Stage, diagnostic.Reason) == nil || !validSQLFailureContext(&diagnostic) || !validWriteFailureContext(&diagnostic) {
		return nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(payload, &fields) != nil {
		return nil
	}
	for _, name := range []string{"sql", "write", "target_change_possible"} {
		if value, ok := fields[name]; ok && bytes.Equal(value, []byte("null")) {
			return nil
		}
	}
	return &diagnostic
}
