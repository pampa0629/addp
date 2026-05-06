package metaitem

import "github.com/addp/common/dataitem"

// BuildAttributes 将 Meta 扫描得到的 item 语义合并为可落库 attributes。
func BuildAttributes(item *dataitem.DetectedItem) map[string]interface{} {
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

func mergeAttributeSection(existing interface{}, additions map[string]interface{}) map[string]interface{} {
	merged := map[string]interface{}{}
	if section, ok := existing.(map[string]interface{}); ok {
		for k, v := range section {
			merged[k] = v
		}
	}
	for k, v := range additions {
		merged[k] = v
	}
	return merged
}
