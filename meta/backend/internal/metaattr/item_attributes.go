package metaattr

import (
	"github.com/addp/common/datatype"
)

type DataItemAttributesInput struct {
	Attributes   map[string]interface{}
	Layout       string
	DataType     datatype.DataType
	Format       string
	PhysicalPath string
	RefList      []ItemRefAttributesInput
	SizeBytes    *int64
}

type ItemRefAttributesInput struct {
	Role      string
	Path      string
	Required  bool
	Primary   bool
	Extension string
}

// BuildAttributes 将 Meta 扫描得到的 item 语义合并为可落库 attributes。
func BuildAttributes(item DataItemAttributesInput) map[string]interface{} {
	return BuildAttributesFromInput(item)
}

func BuildAttributesFromInput(item DataItemAttributesInput) map[string]interface{} {
	attrs := make(map[string]interface{}, len(item.Attributes)+10)
	for k, v := range item.Attributes {
		attrs[k] = v
	}

	itemAttrs := map[string]interface{}{}
	storageAttrs := map[string]interface{}{}

	itemAttrs["layout"] = item.Layout
	itemAttrs["data_type"] = string(item.DataType)
	if item.Format != "" {
		itemAttrs["format"] = item.Format
	}
	if item.PhysicalPath != "" {
		storageAttrs["physical_path"] = item.PhysicalPath
	}
	if item.Layout == "multi" && len(item.RefList) > 0 {
		itemAttrs["refs"] = refAttributes(item.RefList)
		itemAttrs["file_count"] = len(item.RefList)
	}
	if item.Layout == "whole" {
		itemAttrs["scope_exclusive"] = true
		itemAttrs["claim_policy"] = "whole_scope"
	}
	if item.SizeBytes != nil && *item.SizeBytes > 0 {
		storageAttrs["total_size"] = *item.SizeBytes
	}
	setMergedAttributeSection(attrs, "item", itemAttrs)
	setMergedAttributeSection(attrs, "storage", storageAttrs)
	return attrs
}

func refAttributes(refs []ItemRefAttributesInput) []map[string]interface{} {
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

func MergeDataItemAttributes(attrs map[string]interface{}, item DataItemAttributesInput) {
	if attrs == nil {
		return
	}
	MergeAttributeMaps(attrs, BuildAttributes(item))
}

func MergeAttributeMaps(attrs map[string]interface{}, additions map[string]interface{}) {
	if attrs == nil {
		return
	}
	for k, v := range additions {
		if isFlatStorageAttributeKey(k) {
			continue
		}
		switch k {
		case "storage", "item", "type_info", "format_info", "content_index", "capabilities":
			setMergedAttributeSection(attrs, k, v)
		default:
			if cleanValue := cleanAttributeValue(v); cleanValue != nil {
				attrs[k] = cleanValue
			}
		}
	}
}

func setMergedAttributeSection(attrs map[string]interface{}, key string, additions interface{}) {
	merged := mergeAttributeSection(attrs[key], additions)
	if len(merged) > 0 {
		attrs[key] = merged
	} else {
		delete(attrs, key)
	}
}

func mergeAttributeSection(existing interface{}, additions interface{}) map[string]interface{} {
	merged := map[string]interface{}{}
	for k, v := range interfaceMap(existing) {
		if cleanValue := cleanAttributeValue(v); cleanValue != nil {
			merged[k] = cleanValue
		}
	}
	for k, v := range interfaceMap(additions) {
		if isFlatStorageAttributeKey(k) {
			continue
		}
		if cleanValue := cleanAttributeValue(v); cleanValue != nil {
			merged[k] = cleanValue
		}
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

func isFlatStorageAttributeKey(key string) bool {
	switch key {
	case "bucket", "path", "name", "size", "file_type", "content_type", "last_modified_at", "object_count":
		return true
	default:
		return false
	}
}
