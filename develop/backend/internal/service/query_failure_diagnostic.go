package service

import (
	"context"
	"errors"
	"net"
	"regexp"
	"strings"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/protection"
)

type querySnapshotError struct {
	reason string
	cause  error
}

func (e *querySnapshotError) Error() string { return e.cause.Error() }
func (e *querySnapshotError) Unwrap() error { return e.cause }

var querySQLStatePattern = regexp.MustCompile(`^[0-9A-Z]{5}$`)

func queryErrorCode(err error) string {
	if code := plugin.QueryErrorCodeOf(err); code != "" {
		return string(code)
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) && querySQLStatePattern.MatchString(state.SQLState()) {
		return "sqlstate_" + state.SQLState()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	var network net.Error
	if errors.As(err, &network) {
		return "connection_failed"
	}
	if errors.Is(err, protection.ErrRequired) || errors.Is(err, dataprotection.ErrRoleOptionUnavailable) || errors.Is(err, dataprotection.ErrRoleOptionIneligible) {
		return "develop.query.protection_required"
	}
	return "develop.query.execution_failed"
}

func queryFailureDiagnostic(code, message string, cause error) *commonExecution.FailureDiagnostic {
	stage, reason := "validation", "configuration_invalid"
	switch code {
	case "develop.query.protection_required":
		stage, reason = "preparation", commonExecution.ProtectionFailureReason(cause)
		if errors.Is(cause, protection.ErrResultRequired) {
			stage = "result"
		}
	case "develop.query.snapshot_invalid":
		var snapshot *querySnapshotError
		if errors.As(cause, &snapshot) {
			reason = snapshot.reason
		}
	case "develop.query.authorization_failed":
		stage, reason = "authorization", "authorization_invalid"
	case "develop.query.engine_access_failed":
		stage, reason = "preparation", "engine_unavailable"
	case "develop.query.engine_mismatch":
		reason = "engine_mismatch"
	case "develop.query.runtime_inputs_invalid", "develop.query.relation_parameters_invalid":
		reason = "relation_binding_invalid"
	case "develop.query.relation_compile_failed":
		stage, reason = "preparation", "query_template_invalid"
	case "develop.query.source_invalid", "develop.query.parent_invalid", "develop.query.execution_invalid", "develop.query.engine_invalid":
	default:
		stage, reason = "execution", "query_failed"
		if cause != nil {
			code = queryErrorCode(cause)
		}
		switch code {
		case string(plugin.QueryErrorCodeLocalReadUnsupported):
			stage, reason = "preparation", "local_read_unsupported"
		case string(plugin.QueryErrorCodeSyntaxInvalid):
			stage, reason = "preparation", "query_syntax_invalid"
		case string(plugin.QueryErrorCodeTableNotFound):
			stage, reason = "preparation", "table_not_found"
		case string(plugin.QueryErrorCodeFunctionNotFound):
			stage, reason = "preparation", "function_or_operator_not_found"
		case "sqlstate_42601":
			reason = "query_syntax_invalid"
		case "sqlstate_42703":
			reason = "column_not_found"
		case "sqlstate_42702":
			reason = "ambiguous_column"
		case "sqlstate_42883":
			reason = "function_or_operator_not_found"
		case "sqlstate_22012":
			reason = "division_by_zero"
		case "sqlstate_42P01":
			reason = "table_not_found"
		case "sqlstate_42804", "sqlstate_22P02", "sqlstate_22003", "sqlstate_22007", "sqlstate_22008":
			reason = "type_mismatch"
		case "sqlstate_42501", "permission_denied":
			reason = "permission_denied"
		case "timeout", "cancelled", "connection_failed":
			reason = code
		case "mongodb_database_required":
			stage, reason = "preparation", "database_required"
		default:
			category := commonExecution.FailureCategory(commonModels.JSONMap{"message": message})
			if strings.HasPrefix(code, "sqlstate_08") {
				category = "connection_failed"
			}
			if strings.HasPrefix(code, "sqlstate_53") {
				category = "resource_exhausted"
			}
			switch category {
			case "timeout", "cancelled", "connection_failed", "permission_denied", "resource_exhausted":
				reason = category
			}
		}
	}
	return commonExecution.NewFailureDiagnostic(stage, reason)
}

func queryExecutionFailureDiagnostic(execution *commonExecution.TaskExecution, code string, cause error) *commonExecution.FailureDiagnostic {
	diagnostic := queryFailureDiagnostic(code, cause.Error(), cause)
	context := plugin.QueryErrorContextOf(cause)
	if context == nil {
		return diagnostic
	}
	inputs, _ := mapValue(execution.ExecutionConfig["inputs"])
	digest, _ := inputs["query_source_digest"].(string)
	if digest == "" || context.SourceDigest != digest {
		context.Position = nil
	}
	return commonExecution.WithSQLFailureContext(diagnostic, context)
}
