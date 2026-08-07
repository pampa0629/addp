package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

// Develop 模块消息 key 常量
const (
	MsgExecutionStarted                  = "develop.execution.started"
	MsgExecutionParametersInvalid        = "develop.execution.parameters_invalid"
	MsgExecutionContractUnavailable      = "develop.execution.contract_unavailable"
	MsgExecutionStartFailed              = "develop.execution.start_failed"
	MsgRetryStarted                      = "develop.execution.retry_started"
	MsgParamExecStarted                  = "develop.execution.param_exec_started"
	MsgLogsNotReady                      = "develop.execution.logs_not_ready"
	MsgDeleteSuccess                     = "develop.task.delete_success"
	MsgUseExecuteEndpoint                = "develop.task.use_execute_endpoint"
	MsgInvalidTaskID                     = "develop.task.invalid_id"
	MsgTaskNotFound                      = "develop.task.not_found"
	MsgTaskNotWorkflow                   = "develop.workflow.task_not_workflow"
	MsgInvalidStorageBinding             = "develop.workflow.invalid_storage_binding"
	MsgStorageBindingListFailed          = "develop.workflow.storage_binding_list_failed"
	MsgStorageBindingNotFound            = "develop.workflow.storage_binding_not_found"
	MsgStorageBindingConflict            = "develop.workflow.storage_binding_conflict"
	MsgStorageEngineUnavailable          = "develop.workflow.storage_engine_unavailable"
	MsgStorageEngineIncompatible         = "develop.workflow.storage_engine_incompatible"
	MsgStorageEngineDiscoveryFailed      = "develop.workflow.storage_engine_discovery_failed"
	MsgStorageBindingUpdateFailed        = "develop.workflow.storage_binding_update_failed"
	MsgEngineListFailed                  = "develop.engine.list_failed"
	MsgWorkflowListFailed                = "develop.engine.workflow_list_failed"
	MsgSparkListFailed                   = "develop.engine.spark_list_failed"
	MsgAuthenticationRequired            = "develop.auth.authentication_required"
	MsgSQLClassificationFailed           = "develop.query.effect_unclassifiable"
	MsgControlledSQLEngineUnsupported    = "develop.query.controlled_engine_unsupported"
	MsgDuckDBReadOnly                    = "develop.query.duckdb_read_only"
	MsgExecutionEffectForbidden          = "develop.query.effect_permission_denied"
	MsgExecutionConflict                 = "develop.query.authorization_conflict"
	MsgExecutionAuthorizationUnavailable = "develop.query.authorization_unavailable"
	MsgSQLExecutionFailed                = "develop.query.execution_failed"
	MsgConnectionTestSuccess             = "develop.query.connection_test_success"
	MsgConnectionTestFailed              = "develop.query.connection_test_failed"
	MsgSampleQueryUnavailable            = "develop.query.sample_query_unavailable"
	MsgSampleQueryResourceEmpty          = "develop.query.sample_query_resource_empty"
	MsgQueryTemplateResourceInvalid      = "develop.query.template_resource_invalid"
	MsgQueryParameterDefinitionsInvalid  = "develop.query.parameter_definitions_invalid"
	MsgQueryConfirmationRequired         = "develop.query.confirmation_required"
	MsgNotebookExecutionForbidden        = "develop.notebook.execution_permission_denied"
	MsgNotebookEngineListFailed          = "develop.notebook.engine_list_failed"
	MsgNotebookEngineRequired            = "develop.notebook.engine_required"
	MsgNotebookEngineUnavailable         = "develop.notebook.engine_unavailable"
	MsgNotebookKernelListFailed          = "develop.notebook.kernel_list_failed"
	MsgNotebookKernelRequired            = "develop.notebook.kernel_required"
	MsgNotebookKernelUnavailable         = "develop.notebook.kernel_unavailable"
	MsgNotebookInvalidParameters         = "develop.notebook.invalid_parameters"
	MsgNotebookFileRequired              = "develop.notebook.file_required"
	MsgNotebookStoreFailed               = "develop.notebook.store_failed"
	MsgNotebookCreateFailed              = "develop.notebook.create_failed"
	MsgNotebookInvalidID                 = "develop.notebook.invalid_id"
	MsgNotebookInvalidRuntimeBinding     = "develop.notebook.invalid_runtime_binding"
	MsgNotebookNotFound                  = "develop.notebook.not_found"
	MsgTaskNotNotebook                   = "develop.notebook.task_not_notebook"
	MsgNotebookRuntimeBindingFailed      = "develop.notebook.runtime_binding_failed"
	MsgNotebookSessionConflict           = "develop.notebook.session_conflict"
	MsgNotebookSessionNotFound           = "develop.notebook.session_not_found"
	MsgNotebookSessionOpenFailed         = "develop.notebook.session_open_failed"
	MsgNotebookSessionCloseFailed        = "develop.notebook.session_close_failed"
	MsgNotebookSessionUnavailable        = "develop.notebook.session_unavailable"
	MsgNotebookCatalogRequestInvalid     = "develop.notebook.catalog_request_invalid"
	MsgNotebookCatalogForbidden          = "develop.notebook.catalog_forbidden"
	MsgNotebookSessionControlPlaneFailed = "develop.notebook.catalog_control_plane_failed"
	MsgNotebookCatalogProviderFailed     = "develop.notebook.catalog_provider_failed"
	MsgNotebookCatalogEngineNotFound     = "develop.notebook.catalog_engine_not_found"
	MsgNotebookCatalogEntryNotFound      = "develop.notebook.catalog_entry_not_found"
	MsgNotebookCatalogUnsupported        = "develop.notebook.catalog_unsupported"
	MsgNotebookCatalogEngineUnavailable  = "develop.notebook.catalog_engine_unavailable"
	MsgNotebookCatalogTimeout            = "develop.notebook.catalog_timeout"
	MsgApprovalInvalidRequest            = "develop.approval.invalid_request"
	MsgApprovalInvalidDecision           = "develop.approval.invalid_decision"
	MsgApprovalForbidden                 = "develop.approval.forbidden"
	MsgApprovalNotFound                  = "develop.approval.not_found"
	MsgApprovalExpired                   = "develop.approval.expired"
	MsgApprovalNotApproved               = "develop.approval.not_approved"
	MsgApprovalRejected                  = "develop.approval.rejected"
	MsgApprovalConsumed                  = "develop.approval.already_consumed"
	MsgApprovalMismatch                  = "develop.approval.request_mismatch"
	MsgApprovalUnavailable               = "develop.approval.unavailable"
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
