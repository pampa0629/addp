package metaitem

import "github.com/addp/meta/internal/dataitem"

// BuildAttributes 将 Meta 扫描得到的 item 语义合并为可落库 attributes。
func BuildAttributes(item *DetectedItem) map[string]interface{} {
	if item == nil {
		return map[string]interface{}{}
	}
	attrs := make(map[string]interface{}, len(item.Attributes)+10)
	for k, v := range item.Attributes {
		attrs[k] = v
	}

	itemAttrs := map[string]interface{}{}
	storageAttrs := map[string]interface{}{}

	itemAttrs["organization"] = string(item.Organization)
	itemAttrs["data_type"] = string(item.DataType)
	if item.Format != "" {
		itemAttrs["format"] = item.Format
	}
	if item.PhysicalPath != "" {
		storageAttrs["physical_path"] = item.PhysicalPath
	}
	if item.Organization == dataitem.OrganizationMulti && len(item.ComponentFiles) > 0 {
		itemAttrs["component_files"] = item.ComponentFiles
		itemAttrs["file_count"] = len(item.ComponentFiles)
	}
	if item.Organization == dataitem.OrganizationWhole {
		itemAttrs["scope_exclusive"] = true
		itemAttrs["claim_policy"] = "whole_scope"
	}
	if item.SizeBytes > 0 {
		storageAttrs["total_size"] = item.SizeBytes
	}
	attrs["item"] = mergeAttributeSection(attrs["item"], itemAttrs)
	attrs["storage"] = mergeAttributeSection(attrs["storage"], storageAttrs)
	return attrs
}

func MergeDataItemAttributes(attrs map[string]interface{}, item *DetectedItem) {
	if attrs == nil || item == nil {
		return
	}
	for k, v := range BuildAttributes(item) {
		if isLegacyFlatStorageKey(k) {
			continue
		}
		switch k {
		case "storage", "item", "type_info", "format_info", "capabilities":
			attrs[k] = mergeAttributeSection(attrs[k], v)
		default:
			attrs[k] = v
		}
	}
}

func mergeAttributeSection(existing interface{}, additions interface{}) map[string]interface{} {
	merged := map[string]interface{}{}
	for k, v := range interfaceMap(existing) {
		merged[k] = v
	}
	for k, v := range interfaceMap(additions) {
		if isLegacyFlatStorageKey(k) {
			continue
		}
		merged[k] = v
	}
	return merged
}

func interfaceMap(value interface{}) map[string]interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		return v
	case map[string]string:
		result := map[string]interface{}{}
		for k, item := range v {
			result[k] = item
		}
		return result
	default:
		return map[string]interface{}{}
	}
}

func isLegacyFlatStorageKey(key string) bool {
	switch key {
	case "bucket", "path", "name", "size", "file_type", "content_type", "last_modified_at", "object_count":
		return true
	default:
		return false
	}
}
