package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

// 消息 key 常量
const (
	MsgTokenGenFailed                        = "system.auth.token_gen_failed"
	MsgRegisterDisabled                      = "system.auth.register_disabled"
	MsgMissingAuthHeader                     = "system.auth.missing_auth_header"
	MsgInvalidAuthFormat                     = "system.auth.invalid_auth_format"
	MsgInvalidToken                          = "system.auth.invalid_token"
	MsgAccountUnavailable                    = "system.auth.account_unavailable"
	MsgTokenRefreshFailed                    = "system.auth.token_refresh_failed"
	MsgStepUpRequired                        = "system.auth.step_up_required"
	MsgSessionConflict                       = "system.auth.session_conflict"
	MsgDelegationConflict                    = "system.auth.delegation_conflict"
	MsgExecutionAuthorizationConflict        = "system.auth.execution_authorization_conflict"
	MsgExecutionAuthorizationUnavailable     = "system.auth.execution_authorization_unavailable"
	MsgNotebookSessionAuthorizationConflict  = "system.auth.notebook_session_authorization_conflict"
	MsgNotebookSessionAuthorizationForbidden = "system.auth.notebook_session_authorization_forbidden"
	MsgInternalError                         = "system.auth.internal_error"
	MsgInvalidCurrentPassword                = "system.auth.invalid_current_password"
	MsgPasswordUnchanged                     = "system.auth.password_unchanged"
	MsgTOTPAlreadyEnrolled                   = "system.auth.totp_already_enrolled"
	MsgTOTPEnrollmentRequired                = "system.auth.totp_enrollment_required"
	MsgMFAResetNotAvailable                  = "system.auth.mfa_reset_not_available"
	MsgInvalidMFAVerification                = "system.auth.invalid_mfa_verification"
	MsgRoleAssignmentAlreadyExists           = "system.iam.role_assignment_already_exists"
	MsgRoleAssignmentPrincipalTypeNotAllowed = "system.iam.role_assignment_principal_type_not_allowed"

	MsgLogNotFound          = "system.log.not_found"
	MsgExportFailed         = "system.log.export_failed"
	MsgAuditLogCreateFailed = "system.log.audit_create_failed"
	MsgAuditLogCreated      = "system.log.audit_created"

	MsgModuleNotFound               = "system.module.not_found"
	MsgModuleRegistered             = "system.module.registered"
	MsgModuleHeartbeat              = "system.module.heartbeat"
	MsgModuleDeleted                = "system.module.deleted"
	MsgModuleRegistrationInvalid    = "system.module.registration_invalid"
	MsgModuleRuntimeInstanceMissing = "system.module.runtime_instance_missing"
	MsgModuleVersionConflict        = "system.module.version_conflict"
	MsgTaskProviderNotFound         = "system.task_provider.not_found"

	MsgEngineIdentityImmutable           = "system.engine.identity_immutable"
	MsgEngineDeleting                    = "system.engine.deleting"
	MsgEngineDeleted                     = "system.engine.deleted"
	MsgEngineRestoreRequired             = "system.engine.restore_required"
	MsgEngineVersionConflict             = "system.engine.version_conflict"
	MsgEngineLifecycleInvalid            = "system.engine.lifecycle_invalid"
	MsgEngineArtifactPolicyInvalid       = "system.engine.artifact_policy_invalid"
	MsgEngineCleanupUnavailable          = "system.engine.cleanup_unavailable"
	MsgEngineDeletionStarted             = "system.engine.deletion_started"
	MsgEngineDeletionAssessmentInvalid   = "system.engine.deletion_assessment_invalid"
	MsgEngineDeletionAssessmentPending   = "system.engine.deletion_assessment_pending"
	MsgEngineDeletionAssessmentExpired   = "system.engine.deletion_assessment_expired"
	MsgEngineDeletionImpactChanged       = "system.engine.deletion_impact_changed"
	MsgEngineDeletionRunningExecutions   = "system.engine.deletion_running_executions"
	MsgEngineDeletionConfirmationInvalid = "system.engine.deletion_confirmation_invalid"
	MsgCatalogControlPlaneFailed         = "system.catalog.control_plane_failed"
	MsgCatalogProviderFailed             = "system.catalog.provider_failed"
	MsgCatalogEngineNotFound             = "system.catalog.engine_not_found"
	MsgCatalogEntryNotFound              = "system.catalog.entry_not_found"
	MsgCatalogOperationUnsupported       = "system.catalog.operation_unsupported"
	MsgCatalogEngineUnavailable          = "system.catalog.engine_unavailable"
	MsgCatalogTimeout                    = "system.catalog.timeout"

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
