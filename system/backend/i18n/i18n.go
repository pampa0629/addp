package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

// 消息 key 常量
const (
	MsgTokenGenFailed     = "system.auth.token_gen_failed"
	MsgRegisterDisabled   = "system.auth.register_disabled"
	MsgMissingAuthHeader  = "system.auth.missing_auth_header"
	MsgInvalidAuthFormat  = "system.auth.invalid_auth_format"
	MsgInvalidToken       = "system.auth.invalid_token"
	MsgTokenRefreshFailed = "system.auth.token_refresh_failed"

	MsgLogNotFound          = "system.log.not_found"
	MsgExportFailed         = "system.log.export_failed"
	MsgAuditLogCreateFailed = "system.log.audit_create_failed"
	MsgAuditLogCreated      = "system.log.audit_created"

	MsgModuleNotFound  = "system.module.not_found"
	MsgModuleRegistered = "system.module.registered"
	MsgModuleHeartbeat = "system.module.heartbeat"
	MsgModuleDeleted   = "system.module.deleted"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
