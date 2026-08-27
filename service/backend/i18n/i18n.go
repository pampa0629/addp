package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

// Service 模块消息 key 常量
const (
	MsgServiceNameExists           = "service.err.name_exists"
	MsgLayerRequired               = "service.err.layer_required"
	MsgUnsupportedType             = "service.err.unsupported_type"
	MsgDeleteSuccess               = "service.success.deleted"
	MsgMissingRef                  = "service.err.missing_ref"
	MsgInvalidRefFormat            = "service.err.invalid_ref_format"
	MsgInvalidRefID                = "service.err.invalid_ref_id"
	MsgServiceNotFound             = "service.err.not_found"
	MsgServiceLookupFailed         = "service.err.lookup_failed"
	MsgServiceInactive             = "service.err.inactive"
	MsgRESTAPIDisabled             = "service.err.rest_api_disabled"
	MsgSnapshotCheckFailed         = "service.err.snapshot_check_failed"
	MsgSnapshotRefreshFailed       = "service.err.snapshot_refresh_failed"
	MsgSQLOutputContractFailed     = "service.err.sql_output_contract_failed"
	MsgAuthenticationRequired      = "service.err.authentication_required"
	MsgQuerySampleForbidden        = "service.err.query_sample_forbidden"
	MsgQuerySampleUnavailable      = "service.err.query_sample_unavailable"
	MsgQuerySampleFailed           = "service.err.query_sample_failed"
	MsgInvalidQueryRequest         = "service.err.invalid_query_request"
	MsgInvalidQueryFormat          = "service.err.invalid_query_format"
	MsgInvalidQueryIntent          = "service.err.invalid_query_intent"
	MsgInvalidStructuredQuery      = "service.err.invalid_structured_query"
	MsgQueryExecutionFailed        = "service.err.query_execution_failed"
	MsgQueryFormatFailed           = "service.err.query_format_failed"
	MsgInvalidFeatureID            = "service.err.invalid_feature_id"
	MsgConsumerFilterInvalid       = "service.consumer.filter_invalid"
	MsgConsumerReferenceInvalid    = "service.consumer.reference_invalid"
	MsgConsumerCatalogFailed       = "service.consumer.catalog_failed"
	MsgConfigurationLoadFailed     = "service.configuration.load_failed"
	MsgConfigurationInvalid        = "service.configuration.invalid"
	MsgConfigurationAuthentication = "service.configuration.authentication_required"
	MsgConfigurationConflict       = "service.configuration.version_conflict"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
