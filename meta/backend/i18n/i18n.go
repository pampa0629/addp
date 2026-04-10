package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

// Meta 模块消息 key 常量
const (
	MsgCacheCleared    = "meta.cache.cleared"
	MsgCacheClearedAll = "meta.cache.cleared_all"
	MsgCacheRefreshed  = "meta.cache.refreshed"
	MsgCacheFailed     = "meta.cache.failed"
	MsgCacheCleared1   = "meta.cache.cleared_engine"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
