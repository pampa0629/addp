package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

// 通用消息
const (
	MsgInvalidID                  = "standard.common.invalid_id"
	MsgInvalidParams              = "standard.common.invalid_params"
	MsgDeleteSuccess              = "standard.common.delete_success"
	MsgUpdateSuccess              = "standard.common.update_success"
	MsgApproveSuccess             = "standard.common.approve_success"
	MsgDeprecateSuccess           = "standard.common.deprecate_success"
	MsgLinkSuccess                = "standard.common.link_success"
	MsgUnlinkSuccess              = "standard.common.unlink_success"
	MsgUploadSuccess              = "standard.common.upload_success"
	MsgDocIDRequired              = "standard.common.doc_id_required"
	MsgFileRequired               = "standard.common.file_required"
	MsgFileOpenFailed             = "standard.common.file_open_failed"
	MsgFileReadFailed             = "standard.common.file_read_failed"
	MsgOperationFailed            = "standard.common.operation_failed"
	MsgResourceNotFound           = "standard.common.resource_not_found"
	MsgResourceConflict           = "standard.common.resource_conflict"
	MsgInvalidResourceReference   = "standard.common.invalid_resource_reference"
	MsgInvalidCodeSetType         = "standard.code_set.invalid_type"
	MsgDomainParentCycle          = "standard.domain.parent_cycle"
	MsgClassificationParentCycle  = "standard.classification.parent_cycle"
	MsgMetricCategoryParentCycle  = "standard.metric_category.parent_cycle"
	MsgSystemCategoryImmutable    = "standard.measurement_category.system_immutable"
	MsgSystemUnitImmutable        = "standard.unit.system_immutable"
	MsgSystemCodeSetImmutable     = "standard.code_set.system_immutable"
	MsgDocumentStorageUnavailable = "standard.document.storage_unavailable"
	MsgDocumentFileTooLarge       = "standard.document.file_too_large"
	MsgDocumentFileUploadFailed   = "standard.document.file_upload_failed"
	MsgDocumentFileDownloadFailed = "standard.document.file_download_failed"
	MsgDocumentFileCleanupFailed  = "standard.document.file_cleanup_failed"
	MsgDocumentFileNameInvalid    = "standard.document.file_name_invalid"
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
	MsgMetricNotFound        = "standard.metric.not_found"
	MsgMetricDependencyCycle = "standard.metric.dependency_cycle"
)

// DimensionHierarchy
const (
	MsgDimHierarchyNotFound        = "standard.dim_hierarchy.not_found"
	MsgInvalidLevelID              = "standard.dim_hierarchy.invalid_level_id"
	MsgInvalidHierarchyLevelNumber = "standard.dim_hierarchy.invalid_level_number"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
