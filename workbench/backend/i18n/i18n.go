package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

const (
	MsgInvalidRequest                  = "workbench.error.invalid_request"
	MsgViewNotFound                    = "workbench.error.view_not_found"
	MsgVersionConflict                 = "workbench.error.version_conflict"
	MsgServiceAccessDenied             = "workbench.error.service_access_denied"
	MsgServiceUnavailable              = "workbench.error.service_unavailable"
	MsgOperationFailed                 = "workbench.error.operation_failed"
	MsgDeleteSucceeded                 = "workbench.message.delete_succeeded"
	MsgInvalidDataApplication          = "workbench.error.invalid_data_application"
	MsgDataApplicationNotFound         = "workbench.error.data_application_not_found"
	MsgDataApplicationVersionConflict  = "workbench.error.data_application_version_conflict"
	MsgDataApplicationAlreadyPublished = "workbench.error.data_application_already_published"
	MsgDataApplicationNotPublished     = "workbench.error.data_application_not_published"
	MsgDataApplicationAccessDenied     = "workbench.error.data_application_access_denied"
	MsgInvalidResourceGrant            = "workbench.error.invalid_resource_grant"
	MsgResourceGrantConflict           = "workbench.error.resource_grant_conflict"
	MsgDataApplicationDeleteSucceeded  = "workbench.message.data_application_delete_succeeded"
)

func init() { commoni18n.RegisterBundle(localeFS, "locales") }
