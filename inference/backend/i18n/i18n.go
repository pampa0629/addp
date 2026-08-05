package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

const (
	MsgRequestInvalid       = "inference.error.request_invalid"
	MsgScopeForbidden       = "inference.error.scope_forbidden"
	MsgProfileNotFound      = "inference.error.model_profile_not_found"
	MsgResourceInUse        = "inference.error.resource_in_use"
	MsgProfileUnavailable   = "inference.error.model_profile_unavailable"
	MsgOperationUnsupported = "inference.error.operation_unsupported"
	MsgUpstreamUnavailable  = "inference.error.upstream_unavailable"
	MsgTimeout              = "inference.error.timeout"
	MsgUpstreamFailed       = "inference.error.upstream_failed"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
