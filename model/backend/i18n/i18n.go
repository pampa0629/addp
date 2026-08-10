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
	MsgInvalidID = "model.common.invalid_id"

	// 实体
	MsgEntityNotFound       = "model.entity.not_found"
	MsgInvalidEntityID      = "model.entity.invalid_id"
	MsgInvalidAttributeID   = "model.entity.invalid_attribute_id"
	MsgInvalidEntityIDQuery = "model.entity.invalid_entity_id_query"

	// 实体关系
	MsgRelationNotFound = "model.entity_relation.not_found"

	// 逻辑表
	MsgTableNotFound  = "model.logical_table.not_found"
	MsgInvalidFieldID = "model.logical_table.invalid_field_id"

	// 数仓分层
	MsgLayerNotFound = "model.dw_layer.not_found"

	// 指标映射
	MsgInvalidMappingID = "model.fact_metric.invalid_mapping_id"

	// 维度关联
	MsgInvalidRelationID = "model.table_relation.invalid_relation_id"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
