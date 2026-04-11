package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

// 通用消息
const (
	MsgInvalidID     = "standard.common.invalid_id"
	MsgInvalidParams = "standard.common.invalid_params"
	MsgDeleteSuccess = "standard.common.delete_success"
	MsgUpdateSuccess = "standard.common.update_success"
	MsgApproveSuccess = "standard.common.approve_success"
	MsgDeprecateSuccess = "standard.common.deprecate_success"
	MsgLinkSuccess   = "standard.common.link_success"
	MsgUnlinkSuccess = "standard.common.unlink_success"
	MsgUploadSuccess = "standard.common.upload_success"
	MsgDocIDRequired = "standard.common.doc_id_required"
	MsgFileRequired  = "standard.common.file_required"
	MsgFileOpenFailed = "standard.common.file_open_failed"
	MsgFileReadFailed = "standard.common.file_read_failed"
)

// Domain
const (
	MsgDomainNotFound = "standard.domain.not_found"
)

// Glossary
const (
	MsgGlossaryNotFound = "standard.glossary.not_found"
)

// Element
const (
	MsgElementNotFound = "standard.element.not_found"
)

// Document
const (
	MsgDocumentNotFound = "standard.document.not_found"
)

// Unit
const (
	MsgUnitNotFound = "standard.unit.not_found"
)

// Metric
const (
	MsgMetricNotFound = "standard.metric.not_found"
)

// DimensionHierarchy
const (
	MsgDimHierarchyNotFound = "standard.dim_hierarchy.not_found"
	MsgInvalidLevelID       = "standard.dim_hierarchy.invalid_level_id"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
