package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

// Develop 模块消息 key 常量
const (
	MsgExecutionStarted        = "develop.execution.started"
	MsgRetryStarted            = "develop.execution.retry_started"
	MsgParamExecStarted        = "develop.execution.param_exec_started"
	MsgLogsNotReady            = "develop.execution.logs_not_ready"
	MsgDeleteSuccess           = "develop.task.delete_success"
	MsgUseExecuteEndpoint      = "develop.task.use_execute_endpoint"
	MsgEngineListFailed        = "develop.engine.list_failed"
	MsgWorkflowListFailed      = "develop.engine.workflow_list_failed"
	MsgSparkListFailed         = "develop.engine.spark_list_failed"
	MsgApprovalInvalidRequest  = "develop.approval.invalid_request"
	MsgApprovalInvalidDecision = "develop.approval.invalid_decision"
	MsgApprovalForbidden       = "develop.approval.forbidden"
	MsgApprovalNotFound        = "develop.approval.not_found"
	MsgApprovalExpired         = "develop.approval.expired"
	MsgApprovalNotApproved     = "develop.approval.not_approved"
	MsgApprovalRejected        = "develop.approval.rejected"
	MsgApprovalConsumed        = "develop.approval.already_consumed"
	MsgApprovalMismatch        = "develop.approval.request_mismatch"
	MsgApprovalUnavailable     = "develop.approval.unavailable"
)

func ToolApprovalErrorMessageID(code string) string {
	return map[string]string{
		"approval_invalid_request":  MsgApprovalInvalidRequest,
		"approval_invalid_decision": MsgApprovalInvalidDecision,
		"approval_forbidden":        MsgApprovalForbidden,
		"approval_not_found":        MsgApprovalNotFound,
		"approval_expired":          MsgApprovalExpired,
		"approval_not_approved":     MsgApprovalNotApproved,
		"approval_rejected":         MsgApprovalRejected,
		"approval_already_consumed": MsgApprovalConsumed,
		"approval_request_mismatch": MsgApprovalMismatch,
		"approval_unavailable":      MsgApprovalUnavailable,
	}[code]
}

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
