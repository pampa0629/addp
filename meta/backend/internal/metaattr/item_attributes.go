package metaattr

import (
	"github.com/addp/common/dataitem"
	"github.com/addp/meta/internal/metaitem"
)

// BuildAttributes 将 Meta 扫描得到的 item 语义合并为可落库 attributes。
func BuildAttributes(item *metaitem.DetectedItem) map[string]interface{} {
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
	if item.Organization == dataitem.OrganizationMulti && len(item.RefList) > 0 {
		itemAttrs["refs"] = refAttributes(item.RefList)
		itemAttrs["file_count"] = len(item.RefList)
	}
	if item.Organization == dataitem.OrganizationWhole {
		itemAttrs["scope_exclusive"] = true
		itemAttrs["claim_policy"] = "whole_scope"
	}
	if item.Size() > 0 {
		storageAttrs["total_size"] = item.Size()
	}
	attrs["item"] = mergeAttributeSection(attrs["item"], itemAttrs)
	attrs["storage"] = mergeAttributeSection(attrs["storage"], storageAttrs)
	return attrs
}

func refAttributes(refs []dataitem.ItemRef) []map[string]interface{} {
	if len(refs) == 0 {
		return nil
	}
	items := make([]map[string]interface{}, 0, len(refs))
	for _, ref := range refs {
		if ref.Path == "" {
			continue
		}
		item := map[string]interface{}{"path": ref.Path}
		if ref.Role != "" {
			item["role"] = ref.Role
		}
		if ref.Extension != "" {
			item["extension"] = ref.Extension
		}
		if ref.Required {
			item["required"] = true
		}
		if ref.Primary {
			item["primary"] = true
		}
		items = append(items, item)
	}
	return items
}

func MergeDataItemAttributes(attrs map[string]interface{}, item *metaitem.DetectedItem) {
	if attrs == nil || item == nil {
		return
	}
	MergeAttributeMaps(attrs, BuildAttributes(item))
}

func MergeAttributeMaps(attrs map[string]interface{}, additions map[string]interface{}) {
	if attrs == nil {
		return
	}
	for k, v := range additions {
		if isLegacyFlatStorageKey(k) {
			continue
		}
		switch k {
		case "storage", "item", "type_info", "format_info", "content_index", "capabilities":
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
