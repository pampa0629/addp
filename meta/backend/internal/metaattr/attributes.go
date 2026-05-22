package metaattr

import (
	"github.com/addp/common/datatype"
	"strings"

	"github.com/addp/common/format"
	"github.com/addp/meta/internal/models"
)

// Normalize 保留标准 attributes 分区，并补齐 schema_version。
func Normalize(attrs models.JSONMap) models.JSONMap {
	if attrs == nil {
		return nil
	}

	normalized := models.JSONMap{}
	normalized["schema_version"] = 1

	for _, section := range []string{"storage", "item", "type_info", "format_info", "content_index", "capabilities"} {
		normalized[section] = Section(attrs, section)
	}
	return normalized
}

// Section 返回标准 attributes 分区的 JSON map。
func Section(attrs models.JSONMap, key string) map[string]interface{} {
	if section, ok := attrs[key].(map[string]interface{}); ok {
		return section
	}
	if section, ok := attrs[key].(models.JSONMap); ok {
		return map[string]interface{}(section)
	}
	return map[string]interface{}{}
}

// UpsertSection 合并写入标准 attributes 分区。
func UpsertSection(attrs models.JSONMap, key string, values map[string]interface{}) {
	if attrs == nil || len(values) == 0 {
		return
	}
	section := Section(attrs, key)
	for k, v := range values {
		section[k] = v
	}
	attrs[key] = section
}

// SetStorage 写入 storage 分区。
func SetStorage(attrs models.JSONMap, key string, value interface{}) {
	if attrs == nil {
		return
	}
	UpsertSection(attrs, "storage", map[string]interface{}{key: value})
}

// SetItem 写入 item 分区。
func SetItem(attrs models.JSONMap, key string, value interface{}) {
	if attrs == nil {
		return
	}
	UpsertSection(attrs, "item", map[string]interface{}{key: value})
}

// SetExtension 按命名空间写入 type_info / capabilities / format_info。
func SetExtension(attrs models.JSONMap, namespace string, key string, value interface{}) {
	if attrs == nil || namespace == "" || key == "" {
		return
	}
	namespace = strings.ToLower(strings.TrimSpace(namespace))
	switch namespace {
	case "media", "document":
		UpsertNested(attrs, "type_info", namespace, map[string]interface{}{key: value})
	case "spatial", "statistics", "extraction", "semantic", "temporal", "partitioning", "indexing":
		UpsertNested(attrs, "capabilities", namespace, map[string]interface{}{key: value})
	default:
		UpsertNested(attrs, "format_info", "unqualified", map[string]interface{}{key: value})
	}
}

// UpsertNested 合并写入标准分区下的命名空间。
func UpsertNested(attrs models.JSONMap, section string, namespace string, values map[string]interface{}) {
	if attrs == nil || section == "" || namespace == "" || len(values) == 0 {
		return
	}
	sectionAttrs := Section(attrs, section)
	namespaceAttrs := map[string]interface{}{}
	if existing, ok := sectionAttrs[namespace].(map[string]interface{}); ok {
		for k, v := range existing {
			namespaceAttrs[k] = v
		}
	}
	for k, v := range values {
		namespaceAttrs[k] = v
	}
	sectionAttrs[namespace] = namespaceAttrs
	attrs[section] = sectionAttrs
}

func JSONMap(attrs map[string]interface{}) models.JSONMap {
	result := models.JSONMap{}
	for k, v := range attrs {
		result[k] = v
	}
	return result
}

func FieldAttributesFromFormat(fields []format.FieldInfo) []map[string]interface{} {
	fieldsData := make([]map[string]interface{}, 0, len(fields))
	for _, f := range fields {
		field := map[string]interface{}{
			"name":           f.Name,
			"type":           string(f.Type),
			"is_nullable":    f.Nullable,
			"nullable":       f.Nullable,
			"is_primary_key": f.IsPrimaryKey,
			"comment":        f.Comment,
		}
		if datatype.IsSpatialFieldType(f.Type) {
			field["is_spatial"] = true
			field["geometry_type"] = NormalizeGeometryType(string(f.Type))
		}
		fieldsData = append(fieldsData, field)
	}
	return fieldsData
}

func SetSchemaFields(attrs models.JSONMap, fields []map[string]interface{}) {
	if attrs == nil || len(fields) == 0 {
		return
	}
	UpsertNested(attrs, "type_info", "table", map[string]interface{}{"fields": fields})
}
