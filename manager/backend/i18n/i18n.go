package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

// Manager 模块消息 key 常量（目前 manager 后端错误消息均通过 commonAPI 返回英文，此处预留扩展）
const (
	MsgCacheCleared    = "manager.cache.cleared"
	MsgCacheClearedAll = "manager.cache.cleared_all"
	MsgCacheRefreshed  = "manager.cache.refreshed"
	MsgCacheFailed     = "manager.cache.failed"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
