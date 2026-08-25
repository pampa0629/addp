package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

// Model 模块消息 key 常量
const (
	// 通用
	MsgInvalidID                           = "model.common.invalid_id"
	MsgOperationFailed                     = "model.common.operation_failed"
	MsgValidationFailed                    = "model.common.validation_failed"
	MsgResourceNotFound                    = "model.common.resource_not_found"
	MsgResourceConflict                    = "model.common.resource_conflict"
	MsgReferenceNotFound                   = "model.common.reference_not_found"
	MsgStandardUnavailable                 = "model.common.standard_unavailable"
	MsgResourceVersionConflict             = "model.common.resource_version_conflict"
	MsgStandardReferenceDeleting           = "model.common.standard_reference_deleting"
	MsgStandardReferenceGuardStateConflict = "model.common.standard_reference_guard_state_conflict"

	// 实体
	MsgEntityNotFound           = "model.entity.not_found"
	MsgInvalidEntityID          = "model.entity.invalid_id"
	MsgInvalidAttributeID       = "model.entity.invalid_attribute_id"
	MsgInvalidEntityIDQuery     = "model.entity.invalid_entity_id_query"
	MsgEntityCodeConflict       = "model.entity.code_conflict"
	MsgEntityStateConflict      = "model.entity.state_conflict"
	MsgEntityAttributesRequired = "model.entity.approval_attributes_required"
	MsgEntityPrimaryKeyRequired = "model.entity.approval_primary_key_required"
	MsgEntityAttributeInvalid   = "model.entity.approval_attribute_invalid"
	MsgAttributeNotFound        = "model.entity.attribute_not_found"
	MsgAttributeColumnConflict  = "model.entity.attribute_column_conflict"
	MsgRelationTargetNotFound   = "model.entity_relation.target_not_found"
	MsgRelationStateConflict    = "model.entity_relation.state_conflict"
	MsgRelationSelfConflict     = "model.entity_relation.self_conflict"
	MsgRelationConflict         = "model.entity_relation.conflict"

	// 实体关系
	MsgRelationNotFound = "model.entity_relation.not_found"

	// 逻辑表
	MsgTableNotFound               = "model.logical_table.not_found"
	MsgInvalidFieldID              = "model.logical_table.invalid_field_id"
	MsgDDLPreviewInvalid           = "model.logical_table.ddl_preview_invalid"
	MsgTableCodeConflict           = "model.logical_table.code_conflict"
	MsgTableStateConflict          = "model.logical_table.state_conflict"
	MsgTableFieldsRequired         = "model.logical_table.approval_fields_required"
	MsgTablePrimaryKeyRequired     = "model.logical_table.approval_primary_key_required"
	MsgFieldNotFound               = "model.logical_table.field_not_found"
	MsgFieldColumnConflict         = "model.logical_table.field_column_conflict"
	MsgTableRelationTargetNotFound = "model.table_relation.target_not_found"
	MsgTableRelationStateConflict  = "model.table_relation.state_conflict"
	MsgTableRelationConflict       = "model.table_relation.conflict"

	// 逻辑表物化
	MsgMaterializationInvalid     = "model.materialization.invalid"
	MsgMaterializationConflict    = "model.materialization.conflict"
	MsgMaterializationNotFound    = "model.materialization.not_found"
	MsgMaterializationUnavailable = "model.materialization.unavailable"

	// 数仓分层
	MsgLayerNotFound     = "model.dw_layer.not_found"
	MsgLayerCodeConflict = "model.dw_layer.code_conflict"
	MsgLayerInUse        = "model.dw_layer.in_use"

	// 指标映射
	MsgInvalidMappingID = "model.fact_metric.invalid_mapping_id"
	MsgMetricConflict   = "model.fact_metric.conflict"

	// 维度关联
	MsgInvalidRelationID = "model.table_relation.invalid_relation_id"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
