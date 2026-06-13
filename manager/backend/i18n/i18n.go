package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

// Manager 模块消息 key 常量（目前 manager 后端错误消息均通过 commonAPI 返回英文，此处预留扩展）
const (
	MsgCacheCleared                = "manager.cache.cleared"
	MsgCacheClearedAll             = "manager.cache.cleared_all"
	MsgCacheRefreshed              = "manager.cache.refreshed"
	MsgCacheFailed                 = "manager.cache.failed"
	MsgInvalidEngineID             = "manager.error.invalid_engine_id"
	MsgInvalidEngineIDParam        = "manager.error.invalid_engine_id_parameter"
	MsgMissingLocator              = "manager.error.missing_locator"
	MsgEngineAccessDenied          = "manager.error.engine_access_denied"
	MsgMetaScanRequired            = "manager.error.meta_scan_required"
	MsgMissingEngineIDOrStorageRef = "manager.error.missing_engine_id_or_storage_ref"
	MsgSearchKeywordTooShort       = "manager.error.search_keyword_too_short"
	MsgSchemaRequired              = "manager.error.schema_required"
	MsgTableRequired               = "manager.error.table_required"
	MsgSchemaAndTableRequired      = "manager.error.schema_and_table_required"
	MsgSystemClientUnavailable     = "manager.error.system_client_unavailable"
	MsgSystemClientNotInitialized  = "manager.error.system_client_not_initialized"
	MsgEngineNotFound              = "manager.error.engine_not_found"
	MsgUnsupportedPostgresOnly     = "manager.error.unsupported_postgres_only"
	MsgDatabaseConnectionFailed    = "manager.error.database_connection_failed"
	MsgFeatureNotFound             = "manager.error.feature_not_found"
	MsgFeatureInvalidGeometry      = "manager.error.feature_invalid_geometry"
	MsgInvalidZParam               = "manager.error.invalid_z_parameter"
	MsgInvalidXParam               = "manager.error.invalid_x_parameter"
	MsgInvalidYParam               = "manager.error.invalid_y_parameter"
	MsgInvalidSRIDParam            = "manager.error.invalid_srid_parameter"
	MsgInvalidGeoJSON              = "manager.error.invalid_geojson"
	MsgQueryFailed                 = "manager.error.query_failed"
	MsgQueryCountFailed            = "manager.error.query_count_failed"
	MsgQueryExtentFailed           = "manager.error.query_extent_failed"
	MsgParseFormFailed             = "manager.error.parse_form_failed"
	MsgFileRequired                = "manager.error.file_required"
	MsgReadFileFailed              = "manager.error.read_file_failed"
	MsgTargetEngineIDRequired      = "manager.error.target_engine_id_required"
	MsgInvalidTargetEngineID       = "manager.error.invalid_target_engine_id"
	MsgLocatorEngineMismatch       = "manager.error.locator_engine_mismatch"
	MsgItemRefreshRequiresLocator  = "manager.error.item_refresh_requires_locator"
	MsgItemRefreshTargetRequired   = "manager.error.item_refresh_target_required"
	MsgMissingParam                = "manager.error.missing_param"
	MsgInvalidParam                = "manager.error.invalid_param"
	MsgHybridSearchNotConfigured   = "manager.error.hybrid_search_not_configured"
	MsgMissingQuery                = "manager.error.missing_query"
	MsgSearchHistoryUnavailable    = "manager.error.search_history_unavailable"
	MsgUnauthorized                = "manager.error.unauthorized"
	MsgLoadHistoryFailed           = "manager.error.load_history_failed"
	MsgInvalidHistoryID            = "manager.error.invalid_history_id"
	MsgDeleteHistoryFailed         = "manager.error.delete_history_failed"
	MsgClearHistoryFailed          = "manager.error.clear_history_failed"
	MsgInvalidRequestBody          = "manager.error.invalid_request_body"
	MsgQuickViewRecordNotFound     = "manager.error.quick_view_record_not_found"
	MsgQuickViewInvalidMode        = "manager.error.quick_view_invalid_mode"
	MsgQuickViewGeometryMissing    = "manager.error.quick_view_geometry_missing"
	MsgImportTableNameRequired     = "manager.error.import_table_name_required"
	MsgImportZipRequired           = "manager.error.import_zip_required"
	MsgImportUnsupportedFormat     = "manager.error.import_unsupported_format"
	MsgImportSourceNotConfigured   = "manager.error.import_source_not_configured"
	MsgImportSourceNotMatched      = "manager.error.import_source_not_matched"
	MsgImportSourceAmbiguous       = "manager.error.import_source_ambiguous"
	MsgImportSourceIDInvalid       = "manager.error.import_source_id_invalid"
	MsgImportSourceInactive        = "manager.error.import_source_inactive"
	MsgImportZipMissingShp         = "manager.error.import_zip_missing_shp"
	MsgImportZipBasenameMismatch   = "manager.error.import_zip_basename_mismatch"
	MsgImportZipMissingRequired    = "manager.error.import_zip_missing_required"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
