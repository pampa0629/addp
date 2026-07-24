package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

// 消息 key 常量
const (
	MsgTokenGenFailed         = "system.auth.token_gen_failed"
	MsgRegisterDisabled       = "system.auth.register_disabled"
	MsgMissingAuthHeader      = "system.auth.missing_auth_header"
	MsgInvalidAuthFormat      = "system.auth.invalid_auth_format"
	MsgInvalidToken           = "system.auth.invalid_token"
	MsgAccountUnavailable     = "system.auth.account_unavailable"
	MsgTokenRefreshFailed     = "system.auth.token_refresh_failed"
	MsgStepUpRequired         = "system.auth.step_up_required"
	MsgSessionConflict        = "system.auth.session_conflict"
	MsgInternalError          = "system.auth.internal_error"
	MsgInvalidCurrentPassword = "system.auth.invalid_current_password"
	MsgPasswordUnchanged      = "system.auth.password_unchanged"

	MsgLogNotFound          = "system.log.not_found"
	MsgExportFailed         = "system.log.export_failed"
	MsgAuditLogCreateFailed = "system.log.audit_create_failed"
	MsgAuditLogCreated      = "system.log.audit_created"

	MsgModuleNotFound   = "system.module.not_found"
	MsgModuleRegistered = "system.module.registered"
	MsgModuleHeartbeat  = "system.module.heartbeat"
	MsgModuleDeleted    = "system.module.deleted"

	MsgCleanupConfirmRequired      = "system.cleanup.confirm_required"
	MsgCleanupConfirmTokenRequired = "system.cleanup.confirm_token_required"
	MsgCleanupTenantRequired       = "system.cleanup.tenant_required"
	MsgCleanupTenantMissing        = "system.cleanup.tenant_missing"
	MsgCleanupCreateScanFailed     = "system.cleanup.create_scan_failed"
	MsgCleanupTaskIDRequired       = "system.cleanup.task_id_required"
	MsgCleanupTaskNotFound         = "system.cleanup.task_not_found"
	MsgCleanupGetTaskFailed        = "system.cleanup.get_task_failed"
	MsgCleanupTaskForbidden        = "system.cleanup.task_forbidden"
	MsgCleanupScanNotFound         = "system.cleanup.scan_not_found"
	MsgCleanupGetScanFailed        = "system.cleanup.get_scan_failed"
	MsgCleanupExecuteForbidden     = "system.cleanup.execute_forbidden"
	MsgCleanupBasedOnScanRequired  = "system.cleanup.based_on_scan_required"
	MsgCleanupScanNotCompleted     = "system.cleanup.scan_not_completed"
	MsgCleanupCreateExecuteFailed  = "system.cleanup.create_execute_failed"
	MsgCleanupGetHistoryFailed     = "system.cleanup.get_history_failed"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
