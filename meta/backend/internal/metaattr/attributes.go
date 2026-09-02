package metaattr

import (
	"strings"

	"github.com/addp/common/datatype"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/models"
)

// Normalize 保留标准 attributes 分区，并补齐 schema_version。
func Normalize(attrs models.JSONMap) models.JSONMap {
	if attrs == nil {
		return nil
	}

	normalized := models.JSONMap{}
	normalized["schema_version"] = 1

	for _, section := range []string{"storage", "item", "type_info", "format_info", "access_index", "capabilities"} {
		if sectionAttrs := cleanAttributeValue(Section(attrs, section)); sectionAttrs != nil {
			normalized[section] = sectionAttrs
		}
	}
	removePersistedContentDerivatives(normalized)
	return normalized
}

func removePersistedContentDerivatives(attrs models.JSONMap) {
	capabilities := Section(attrs, "capabilities")
	extraction := commonJSON.InterfaceMap(capabilities["extraction"])
	if len(extraction) == 0 {
		return
	}
	delete(extraction, "plain_text_preview")
	delete(extraction, "text_excerpt")
	if cleaned := cleanAttributeMap(extraction); len(cleaned) > 0 {
		capabilities["extraction"] = cleaned
	} else {
		delete(capabilities, "extraction")
	}
	if cleaned := cleanAttributeMap(capabilities); len(cleaned) > 0 {
		attrs["capabilities"] = cleaned
	} else {
		delete(attrs, "capabilities")
	}
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
	case "spatial", "statistics", "extraction", "semantic", "temporal", "constraints", "partitioning", "indexing":
		UpsertNested(attrs, "capabilities", namespace, map[string]interface{}{key: value})
	default:
		UpsertNested(attrs, "format_info", "unqualified", map[string]interface{}{key: value})
	}
}

// ReplaceCapabilityNamespace replaces one authoritative capability facts section.
func ReplaceCapabilityNamespace(attrs models.JSONMap, namespace string, values map[string]interface{}) {
	if attrs == nil || namespace == "" {
		return
	}
	capabilities := Section(attrs, "capabilities")
	delete(capabilities, namespace)
	if cleaned := cleanAttributeMap(values); len(cleaned) > 0 {
		capabilities[namespace] = cleaned
	}
	if capabilities = cleanAttributeMap(capabilities); len(capabilities) > 0 {
		attrs["capabilities"] = capabilities
	} else {
		delete(attrs, "capabilities")
	}
}

func SetExtraction(attrs models.JSONMap, key string, value interface{}) {
	if attrs == nil || key == "" || value == nil {
		return
	}
	SetExtension(attrs, "extraction", key, value)
}

func MergeStandardAttributes(attrs models.JSONMap, additions map[string]interface{}) {
	if attrs == nil || len(additions) == 0 {
		return
	}
	for _, section := range []string{"storage", "item", "type_info", "format_info", "access_index", "capabilities"} {
		values := commonJSON.InterfaceMap(additions[section])
		if len(values) == 0 {
			continue
		}
		MergeAttributeSection(attrs, section, values)
	}
}

func FormatInfoAttributes(formatName string, values map[string]interface{}) map[string]interface{} {
	if len(values) == 0 {
		return nil
	}
	formatName = strings.ToLower(strings.TrimSpace(formatName))
	if formatName == "" {
		formatName = "unqualified"
	}
	if formatInfo := commonJSON.InterfaceMap(values["format_info"]); len(formatInfo) > 0 {
		if scoped := commonJSON.InterfaceMap(formatInfo[formatName]); len(scoped) > 0 {
			return map[string]interface{}{
				"format_info": map[string]interface{}{formatName: scoped},
			}
		}
		return map[string]interface{}{"format_info": formatInfo}
	}
	scoped := map[string]interface{}{}
	for key, value := range values {
		switch key {
		case "storage", "item", "type_info", "format_info", "access_index", "capabilities":
			continue
		default:
			scoped[key] = value
		}
	}
	if scoped = cleanAttributeMap(scoped); len(scoped) == 0 {
		return nil
	}
	return map[string]interface{}{
		"format_info": map[string]interface{}{
			formatName: scoped,
		},
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

func MergeAttributeSection(attrs models.JSONMap, section string, values map[string]interface{}) {
	if attrs == nil || section == "" || len(values) == 0 {
		return
	}
	sectionAttrs := Section(attrs, section)
	for namespace, value := range values {
		if valueMap := commonJSON.InterfaceMap(value); len(valueMap) > 0 {
			namespaceAttrs := map[string]interface{}{}
			for key, existingValue := range commonJSON.InterfaceMap(sectionAttrs[namespace]) {
				namespaceAttrs[key] = existingValue
			}
			for key, newValue := range valueMap {
				namespaceAttrs[key] = newValue
			}
			sectionAttrs[namespace] = namespaceAttrs
			continue
		}
		sectionAttrs[namespace] = value
	}
	if sectionAttrs = cleanAttributeMap(sectionAttrs); len(sectionAttrs) > 0 {
		attrs[section] = sectionAttrs
	} else {
		delete(attrs, section)
	}
}

func RemoveAccessIndexTable(attrs map[string]interface{}) {
	if attrs == nil {
		return
	}
	accessIndex := commonJSON.InterfaceMap(attrs["access_index"])
	if len(accessIndex) == 0 {
		delete(attrs, "access_index")
		return
	}
	delete(accessIndex, "table")
	if cleaned := cleanAttributeMap(accessIndex); len(cleaned) > 0 {
		attrs["access_index"] = cleaned
	} else {
		delete(attrs, "access_index")
	}
}

func JSONMap(attrs map[string]interface{}) models.JSONMap {
	result := models.JSONMap{}
	for k, v := range attrs {
		result[k] = v
	}
	return result
}

func SetTableFields(attrs models.JSONMap, fields []datatype.FieldInfo) {
	if attrs == nil || len(fields) == 0 {
		return
	}
	UpsertNested(attrs, "type_info", "table", datatype.TableInfoPayload(&datatype.TableInfo{Fields: fields}))
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
