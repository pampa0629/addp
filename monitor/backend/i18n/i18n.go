package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

// Monitor 模块消息 key 常量
const (
	MsgTenantNotFound     = "monitor.auth.tenant_not_found"
	MsgInvalidExecutionID = "monitor.execution.invalid_id"
	MsgExecutionNotFound  = "monitor.execution.not_found"
	MsgModuleNotFound     = "monitor.health.module_not_found"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
