package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	"github.com/addp/develop/backend/internal/protection"
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

func TestQueryProtectionFailureUsesTypedBoundary(t *testing.T) {
	for _, scenario := range []struct {
		cause error
		stage string
	}{
		{fmt.Errorf("wrapped: %w: private-runtime-value", protection.ErrRequired), "preparation"},
		{fmt.Errorf("wrapped: %w: private-runtime-value", protection.ErrResultRequired), "result"},
	} {
		code := queryErrorCode(scenario.cause)
		diagnostic := queryFailureDiagnostic(code, scenario.cause.Error(), scenario.cause)
		if code != "develop.query.protection_required" || diagnostic == nil || diagnostic.Stage != scenario.stage || diagnostic.Reason != "data_protection_required" {
			t.Fatalf("protection failure classified as %s, %+v", code, diagnostic)
		}
	}

	// Catalog resolution can fail while the protection gate resolves ReadSet.
	// Preserve the provider's proven cause rather than masking it as protection.
	for _, providerCode := range []plugin.QueryErrorCode{plugin.QueryErrorCodeTableNotFound, plugin.QueryErrorCodeFunctionNotFound} {
		cause := fmt.Errorf("%w: %w", protection.ErrRequired, plugin.NewQueryError(providerCode, errors.New("private runtime body")))
		if code := queryErrorCode(cause); code != string(providerCode) {
			t.Fatalf("lost provider cause %s: %s", providerCode, code)
		}
	}
	// Runtime text alone cannot prove a protection failure.
	fake := errors.New("develop data protection gate is required")
	if code := queryErrorCode(fake); code == "develop.query.protection_required" {
		t.Fatal("inferred protection from free text")
	}
}

func TestQueryProtectionDetailsKeepStageAndStableCode(t *testing.T) {
	for _, item := range []struct {
		cause  error
		reason string
	}{
		{dataprotection.ErrComputedOutputUnsupported, "protection_computed_output_unsupported"},
		{dataprotection.ErrExpired, "protection_expired"},
		{dataprotection.ErrNotYetValid, "protection_not_yet_valid"},
		{dataprotection.ErrNotReady, "protection_not_ready"},
		{dataprotection.ErrSchemaMismatch, "protection_schema_mismatch"},
		{dataprotection.ErrOperationUnsupported, "protection_operation_unsupported"},
		{plugin.ErrQueryOutputLineageUnresolved, "protection_lineage_unverified"},
	} {
		for _, boundary := range []error{protection.ErrRequired, protection.ErrResultRequired} {
			cause := fmt.Errorf("%w: private runtime value: %w", boundary, item.cause)
			code := queryErrorCode(cause)
			diagnostic := queryFailureDiagnostic(code, cause.Error(), cause)
			stage := "preparation"
			if boundary == protection.ErrResultRequired {
				stage = "result"
			}
			if code != "develop.query.protection_required" || diagnostic == nil || diagnostic.Reason != item.reason || diagnostic.Stage != stage {
				t.Fatalf("wrong protection fact: %s %+v", code, diagnostic)
			}
		}
	}
}

func TestCypherScopeFailureHasControlledDiagnostic(t *testing.T) {
	cause := plugin.NewQueryError(plugin.QueryErrorCodeLocalReadUnsupported, errors.New("untrusted query detail"))
	diagnostic := queryFailureDiagnostic(queryErrorCode(cause), cause.Error(), cause)
	if diagnostic == nil || diagnostic.Stage != "preparation" || diagnostic.Reason != "local_read_unsupported" {
		t.Fatalf("diagnostic=%+v", diagnostic)
	}
}
