package execution_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	commonquery "github.com/addp/common/query"
)

func TestSQLFailureContextSurvivesPersistenceWithoutInternalEvidence(t *testing.T) {
	diagnostic := execution.WithSQLFailureContext(execution.NewFailureDiagnostic("preparation", "table_not_found"), commonquery.NewErrorContext("SELECT * FROM public.farmla1nd", "table", "public.farmla1nd", 14))
	payload, _ := json.Marshal(diagnostic)
	var value interface{}
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatal(err)
	}
	observation := execution.Observe(&execution.TaskExecution{Status: "failed", ErrorDetails: models.JSONMap{"diagnostic": value, "message": "private runtime body"}})
	encoded, _ := json.Marshal(observation.ErrorDetails)
	for _, required := range []string{"public.farmla1nd", `"line":1`, `"column":15`} {
		if !strings.Contains(string(encoded), required) {
			t.Fatalf("missing SQL fact: %s", encoded)
		}
	}
	for _, forbidden := range []string{"SELECT", "SourceDigest", "Offset", "private runtime"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("unsafe observation: %s", encoded)
		}
	}
}

func TestFailureDiagnosticSurvivesPersistenceWithoutRuntimeText(t *testing.T) {
	item := &execution.TaskExecution{Status: "failed", ErrorDetails: models.JSONMap{
		"code": "develop.query.snapshot_invalid", "message": "SELECT secret-business-value password=credential-sentinel",
		"diagnostic": execution.NewFailureDiagnostic("validation", "query_template_invalid"),
	}}
	payload, err := json.Marshal(item.ErrorDetails)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(payload, &item.ErrorDetails); err != nil {
		t.Fatal(err)
	}
	observation := execution.Observe(item)
	projected, err := json.Marshal(observation.ErrorDetails)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(projected), `"reason":"query_template_invalid"`) || !strings.Contains(string(projected), `"code":"develop.query.snapshot_invalid"`) {
		t.Fatalf("missing controlled failure facts: %s", projected)
	}
	for _, forbidden := range []string{"SELECT", "secret-business-value", "credential-sentinel", "message"} {
		if strings.Contains(string(projected), forbidden) {
			t.Fatalf("unsafe diagnostic: %s", projected)
		}
	}
	item.Status = "success"
	if len(execution.Observe(item).ErrorDetails) != 0 {
		t.Fatal("success retained a failure diagnostic")
	}
}

func TestProtectionFailureReasonsSurviveObservation(t *testing.T) {
	for _, reason := range []string{
		"protection_role_choice_unavailable", "protection_computed_output_unsupported", "protection_lineage_unverified", "protection_not_ready",
		"protection_expired", "protection_not_yet_valid", "protection_schema_mismatch", "protection_operation_unsupported",
	} {
		for _, stage := range []string{"preparation", "result"} {
			diagnostic := execution.NewFailureDiagnostic(stage, reason)
			if diagnostic == nil {
				t.Fatalf("missing controlled protection reason %s", reason)
			}
			observed := execution.Observe(&execution.TaskExecution{Status: "failed", ErrorDetails: models.JSONMap{
				"code": "develop.query.protection_required", "diagnostic": diagnostic, "message": "private-runtime-value",
			}})
			payload, _ := json.Marshal(observed.ErrorDetails)
			if !strings.Contains(string(payload), `"reason":"`+reason+`"`) || strings.Contains(string(payload), "private-runtime-value") {
				t.Fatalf("invalid protected diagnostic: %s", payload)
			}
		}
	}
}

func TestProtectionFailureReasonRequiresTypedEvidence(t *testing.T) {
	for _, item := range []struct {
		cause  error
		reason string
	}{
		{dataprotection.ErrComputedOutputUnsupported, "protection_computed_output_unsupported"},
		{dataprotection.ErrLineageUnverified, "protection_lineage_unverified"},
		{plugin.ErrQueryReadSetUnresolved, "protection_lineage_unverified"},
		{plugin.ErrQueryOutputLineageUnresolved, "protection_lineage_unverified"},
		{dataprotection.ErrExpired, "protection_expired"},
		{dataprotection.ErrNotYetValid, "protection_not_yet_valid"},
		{dataprotection.ErrRoleOptionIneligible, "protection_role_choice_unavailable"},
		{dataprotection.ErrRoleOptionUnavailable, "protection_role_choice_unavailable"},
		{dataprotection.ErrNotReady, "protection_not_ready"},
		{dataprotection.ErrSchemaMismatch, "protection_schema_mismatch"},
		{dataprotection.ErrOperationUnsupported, "protection_operation_unsupported"},
		{plugin.ErrQueryOutputTransformUnsupported, "protection_operation_unsupported"},
		{dataprotection.ErrDenied, "data_protection_required"},
		{errors.New("protection expired, computed output unsupported: private value"), "data_protection_required"},
		{nil, "data_protection_required"},
	} {
		for _, cause := range []error{item.cause, fmt.Errorf("private wrapper: %w", item.cause), errors.Join(errors.New("private"), item.cause)} {
			if got := execution.ProtectionFailureReason(cause); got != item.reason {
				t.Fatalf("got %s want %s", got, item.reason)
			}
		}
	}
}

func TestFailureDiagnosticRejectsUnknownContractAndFreeText(t *testing.T) {
	for _, raw := range []string{
		`{"schema_version":"addp.execution-failure/v1","stage":"validation","reason":"query_template_invalid"}`,
		`{"schema_version":"addp.execution-failure/v2","stage":"execution","reason":"table_write_failed"}`,
		`{"schema_version":"addp.execution-failure/v3","stage":"SELECT secret","reason":"query_template_invalid"}`,
		`{"schema_version":"addp.execution-failure/v3","stage":"validation","reason":"password=secret"}`,
		`{"schema_version":"addp.execution-failure/v3","stage":"validation","reason":"query_template_invalid","message":"business-secret"}`,
		`{"schema_version":"addp.execution-failure/v3","stage":"validation"}`,
		`"arbitrary runtime body"`, `null`,
		`{"schema_version":"addp.execution-failure/v3","stage":"execution","reason":"column_not_found","sql":{"object":{"kind":"table","name":"private"}}}`,
		`{"schema_version":"addp.execution-failure/v3","stage":"execution","reason":"column_not_found","sql":{"position":{"line":1,"column":0}}}`,
		`{"schema_version":"addp.execution-failure/v3","stage":"execution","reason":"column_not_found","sql":{"object":{"kind":"column","name":"a","message":"private"}}}`,
		`{"schema_version":"addp.execution-failure/v3","stage":"execution","reason":"column_not_found","sql":{"position":{"line":1,"column":1},"SourceDigest":"private"}}`,
	} {
		var value interface{}
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		observed := execution.Observe(&execution.TaskExecution{Status: "failed", ErrorDetails: models.JSONMap{"diagnostic": value}})
		if observed.ErrorDetails["diagnostic"] != nil {
			t.Fatalf("accepted unsafe contract: %s", raw)
		}
	}
}

func TestWriteFailureObservationRoundTripAndClosedContext(t *testing.T) {
	possible := true
	diagnostic := execution.NewFailureDiagnostic("execution", "json_invalid")
	diagnostic.Write = &plugin.TableWriteField{ColumnIndex: 3, DeclaredType: datatype.FieldTypeJSON}
	diagnostic.TargetChangePossible = &possible
	payload, _ := json.Marshal(diagnostic)
	var value interface{}
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatal(err)
	}
	observed := execution.Observe(&execution.TaskExecution{Status: "failed", ErrorDetails: models.JSONMap{"diagnostic": value, "message": "permission denied private SELECT password=secret"}})
	if observed.ErrorDetails["category"] != "execution_failed" {
		t.Fatal("private text overrode the proven write cause")
	}
	encoded, _ := json.Marshal(observed.ErrorDetails)
	for _, want := range []string{`"column_index":3`, `"declared_type":"json"`, `"target_change_possible":true`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("missing safe fact %s: %s", want, encoded)
		}
	}
	if strings.Contains(string(encoded), "private") {
		t.Fatal("raw evidence leaked")
	}
	for _, suffix := range []string{
		`"write":{"column_index":0,"declared_type":"json"}`,
		`"write":{"column_index":3,"declared_type":"bytes"}`,
		`"write":{"column_index":3,"declared_type":"json","value":"private"}`,
		`"write":{"column_index":3.5,"declared_type":"json"}`,
		`"write":null`, `"target_change_possible":null`, `"target_change_possible":false`,
		`"target_change_possible":"true"`,
		`"write":{"column_index":3,"declared_type":"json"},"sql":{"position":{"line":1,"column":1}}`,
	} {
		raw := `{"schema_version":"addp.execution-failure/v3","stage":"execution","reason":"json_invalid",` + suffix + `}`
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		observed := execution.Observe(&execution.TaskExecution{Status: "failed", ErrorDetails: models.JSONMap{"diagnostic": value}})
		if observed.ErrorDetails["diagnostic"] != nil {
			t.Fatalf("accepted unsafe context %s", raw)
		}
	}
}

func TestOperatorFailureObservationRoundTripRejectsRawContext(t *testing.T) {
	source := `{"schema_version":"addp.execution-failure/v3","stage":"execution","reason":"operator_field_conflict","operator":{"node_index":2,"operator_id":"batch_buffer","parameter":"input_gdf","field":"distance"}}`
	for _, raw := range []string{source, strings.Replace(source, `"distance"`, `"password=secret"`, 1),
		strings.Replace(source, `"node_index":2`, `"node_index":true`, 1), strings.Replace(source, `"parameter":"input_gdf"`, `"parameter":null`, 1),
		strings.Replace(source, `"node_index":2`, `"node_index":2,"node_id":"private"`, 1),
		strings.Replace(source, `"stage":"execution"`, `"stage":"validation"`, 1),
		strings.Replace(source, `"operator_field_conflict"`, `"execution_failed"`, 1)} {
		var value interface{}
		json.Unmarshal([]byte(raw), &value)
		item := &execution.TaskExecution{Status: "failed", ErrorDetails: models.JSONMap{"message": "password=secret", "diagnostic": value}}
		encoded, _ := json.Marshal(execution.Observe(item).ErrorDetails)
		if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "private") {
			t.Fatal("raw context leaked")
		}
		if (execution.Observe(item).ErrorDetails["diagnostic"] != nil) != (raw == source) {
			t.Fatalf("invalid projection: %s", raw)
		}
	}
}
