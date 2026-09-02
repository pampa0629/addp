package i18n

import (
	"embed"
	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

const (
	MsgInvalidID                             = "security.common.invalid_id"
	MsgInvalidRequest                        = "security.common.invalid_request"
	MsgOperationFailed                       = "security.common.operation_failed"
	MsgNotFound                              = "security.common.not_found"
	MsgConflict                              = "security.common.conflict"
	MsgVersionConflict                       = "security.common.version_conflict"
	MsgDeleteSuccess                         = "security.common.delete_success"
	MsgProjectionCursorConflict              = "security.projection.cursor_conflict"
	MsgNoSupportedFindingsReleaseUnavailable = "security.enrollment.no_supported_findings_release_unavailable"
)

func init() { commoni18n.RegisterBundle(localeFS, "locales") }
