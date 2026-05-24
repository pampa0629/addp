package metaattr

import (
	"strings"

	"github.com/addp/common/datatype"
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
		if sectionAttrs := cleanAttributeValue(Section(attrs, section)); sectionAttrs != nil {
			normalized[section] = sectionAttrs
		}
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
	values = cleanAttributeMap(values)
	if len(values) == 0 {
		return
	}
	section := Section(attrs, key)
	for k, v := range values {
		section[k] = v
	}
	if section = cleanAttributeMap(section); len(section) > 0 {
		attrs[key] = section
	} else {
		delete(attrs, key)
	}
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
	values = cleanAttributeMap(values)
	if len(values) == 0 {
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
	if namespaceAttrs = cleanAttributeMap(namespaceAttrs); len(namespaceAttrs) > 0 {
		sectionAttrs[namespace] = namespaceAttrs
	} else {
		delete(sectionAttrs, namespace)
	}
	if sectionAttrs = cleanAttributeMap(sectionAttrs); len(sectionAttrs) > 0 {
		attrs[section] = sectionAttrs
	} else {
		delete(attrs, section)
	}
}

func JSONMap(attrs map[string]interface{}) models.JSONMap {
	result := models.JSONMap{}
	for k, v := range attrs {
		result[k] = v
	}
	return result
}

func FieldAttributes(fields []datatype.FieldInfo) []map[string]interface{} {
	return datatype.FieldInfoAttributes(fields)
}

func SetSchemaFields(attrs models.JSONMap, fields []map[string]interface{}) {
	if attrs == nil || len(fields) == 0 {
		return
	}
	UpsertNested(attrs, "type_info", "table", map[string]interface{}{"fields": fields})
}

func cleanAttributeMap(values map[string]interface{}) map[string]interface{} {
	if len(values) == 0 {
		return nil
	}
	cleaned := map[string]interface{}{}
	for key, value := range values {
		if cleanValue := cleanAttributeValue(value); cleanValue != nil {
			cleaned[key] = cleanValue
		}
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

func cleanAttributeValue(value interface{}) interface{} {
	switch v := value.(type) {
	case nil:
		return nil
	case map[string]interface{}:
		if cleaned := cleanAttributeMap(v); len(cleaned) > 0 {
			return cleaned
		}
		return nil
	case models.JSONMap:
		if cleaned := cleanAttributeMap(map[string]interface{}(v)); len(cleaned) > 0 {
			return cleaned
		}
		return nil
	case []interface{}:
		cleaned := make([]interface{}, 0, len(v))
		for _, item := range v {
			if cleanItem := cleanAttributeValue(item); cleanItem != nil {
				cleaned = append(cleaned, cleanItem)
			}
		}
		return cleaned
	case []map[string]interface{}:
		cleaned := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if cleanItem := cleanAttributeMap(item); len(cleanItem) > 0 {
				cleaned = append(cleaned, cleanItem)
			}
		}
		return cleaned
	case []string:
		cleaned := make([]string, 0, len(v))
		for _, item := range v {
			if item != "" {
				cleaned = append(cleaned, item)
			}
		}
		return cleaned
	case string:
		if v == "" {
			return nil
		}
		return v
	default:
		return v
	}
}
