package service

import (
	"context"
	"fmt"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	"testing"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/lib/pq"
)

func TestQueryLocationRequiresTheFrozenSourceIdentity(t *testing.T) {
	source := "SELECT missing_field FROM public.a"
	cause := plugin.WithQueryErrorContext(&pq.Error{Code: "42703", Message: "private"}, commonquery.NewErrorContext(source, "column", "missing_field", 7))
	for _, digest := range []string{commonquery.SourceDigest(source), "", commonquery.SourceDigest("SELECT other FROM public.a")} {
		execution := &commonExecution.TaskExecution{ExecutionConfig: commonModels.JSONMap{"inputs": commonModels.JSONMap{"query_source_digest": digest}}}
		diagnostic := queryExecutionFailureDiagnostic(execution, queryErrorCode(cause), cause)
		if diagnostic.SQL == nil || diagnostic.SQL.Object == nil {
			t.Fatal("lost provider object")
		}
		if (diagnostic.SQL.Position != nil) != (digest == commonquery.SourceDigest(source)) {
			t.Fatalf("unproven source location: %+v", diagnostic.SQL)
		}
		response := professionalExecutionResponse(execution)
		inputs, _ := mapValue(response.ExecutionConfig["inputs"])
		if _, present := inputs["query_source_digest"]; present {
			t.Fatal("internal source digest reached Owner response")
		}
		storedInputs, _ := mapValue(execution.ExecutionConfig["inputs"])
		if storedInputs["query_source_digest"] != digest {
			t.Fatal("Owner projection mutated frozen facts")
		}
	}
}

func TestQueryFailuresUseTypedSQLStateWithoutRuntimeValues(t *testing.T) {
	for _, test := range []struct{ state, reason string }{
		{"42703", "column_not_found"}, {"42P01", "table_not_found"},
		{"42702", "ambiguous_column"}, {"42883", "function_or_operator_not_found"},
		{"22012", "division_by_zero"},
		{"42601", "query_syntax_invalid"}, {"22P02", "type_mismatch"},
		{"42501", "permission_denied"}, {"08006", "connection_failed"},
		{"53200", "resource_exhausted"}, {"XX000", "query_failed"},
	} {
		err := fmt.Errorf("query failed: %w", &pq.Error{Code: pq.ErrorCode(test.state), Message: "private value", Detail: "password=private"})
		code := queryErrorCode(err)
		diagnostic := queryFailureDiagnostic(code, err.Error(), nil)
		if code != "sqlstate_"+test.state || diagnostic.Reason != test.reason || diagnostic.Stage != "execution" {
			t.Fatalf("SQLSTATE %s: %s %#v", test.state, code, diagnostic)
		}
	}
	// A SQLSTATE-looking string alone is not provider evidence.
	if code := queryErrorCode(fmt.Errorf("secret SQLSTATE 42703")); code != "develop.query.execution_failed" {
		t.Fatal(code)
	}
	if queryErrorCode(fmt.Errorf("wrapped: %w", context.DeadlineExceeded)) != "timeout" {
		t.Fatal("lost typed timeout")
	}
}

func TestQuerySnapshotFailuresReportActualValidationBoundary(t *testing.T) {
	for _, test := range []struct {
		config commonModels.JSONMap
		reason string
	}{
		{nil, "configuration_missing"},
		{commonModels.JSONMap{"content": commonModels.JSONMap{}}, "timeout_invalid"},
		{commonModels.JSONMap{"content": commonModels.JSONMap{}, "timeout": 45}, "result_limit_invalid"},
		{commonModels.JSONMap{"content": commonModels.JSONMap{}, "timeout": 45, "query_result_limit": 500}, "configuration_invalid"},
		{commonModels.JSONMap{"content": commonModels.JSONMap{}, "engine_id": 9, "timeout": 45, "query_result_limit": 500}, "query_template_invalid"},
	} {
		_, err := devQueryTaskFromExecution(&commonExecution.TaskExecution{ExecutionConfig: test.config})
		if err == nil {
			t.Fatal("invalid snapshot accepted")
		}
		diagnostic := queryFailureDiagnostic("develop.query.snapshot_invalid", err.Error(), err)
		if diagnostic.Stage != "validation" || diagnostic.Reason != test.reason {
			t.Fatalf("diagnostic: %#v", diagnostic)
		}
	}
}
