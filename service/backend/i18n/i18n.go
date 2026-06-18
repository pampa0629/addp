package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

// Service 模块消息 key 常量
const (
	MsgServiceNameExists = "service.err.name_exists"
	MsgLayerRequired     = "service.err.layer_required"
	MsgUnsupportedType   = "service.err.unsupported_type"
	MsgDeleteSuccess     = "service.success.deleted"
	MsgMissingRef        = "service.err.missing_ref"
	MsgInvalidRefFormat  = "service.err.invalid_ref_format"
	MsgInvalidRefID      = "service.err.invalid_ref_id"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
