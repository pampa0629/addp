package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

const (
	MsgInvalidRequest      = "workbench.error.invalid_request"
	MsgViewNotFound        = "workbench.error.view_not_found"
	MsgVersionConflict     = "workbench.error.version_conflict"
	MsgServiceAccessDenied = "workbench.error.service_access_denied"
	MsgServiceUnavailable  = "workbench.error.service_unavailable"
	MsgOperationFailed     = "workbench.error.operation_failed"
	MsgDeleteSucceeded     = "workbench.message.delete_succeeded"
)

func init() { commoni18n.RegisterBundle(localeFS, "locales") }
