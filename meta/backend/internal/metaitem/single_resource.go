package metaitem

import (
	"path/filepath"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/dataitem"
)

// SingleResourceInput 是 single 组织方式 item 推断的输入。
type SingleResourceInput struct {
	Name        string
	Path        string
	Size        int64
	ContentType string
	Format      string
}

// InferSingleResourceItem 基于一个资源推断基础 Meta item 语义。
func InferSingleResourceItem(file plugin.FileEntry) *DetectedItem {
	return InferSingleResource(SingleResourceInput{
		Name:        file.Name,
		Path:        file.Path,
		Size:        file.Size,
		ContentType: file.ContentType,
	})
}

// InferSingleResource 基于单个资源信息推断基础 Meta item 语义。
func InferSingleResource(input SingleResourceInput) *DetectedItem {
	formatName := dataitem.InferFormat(input.Name, input.ContentType, input.Format)
	organization := dataitem.OrganizationSingle
	dataType := dataitem.InferDataType(formatName, input.ContentType)
	if rule, ok := dataitem.MatchBuiltinSingleResourceRule(formatName); ok {
		organization = rule.Organization
		dataType = rule.DataType
	}
	if formatName == string(format.FormatJSON) && isGeoJSONResource(input) {
		dataType = dataitem.DataTypeTable
	}
	item := &DetectedItem{
		Organization:   organization,
		DataType:       dataType,
		Format:         formatName,
		PhysicalPath:   input.Path,
		EntryPath:      input.Path,
		ComponentFiles: []string{input.Path},
		SizeBytes:      input.Size,
		Attributes: map[string]interface{}{
			"storage": map[string]interface{}{
				"path":         input.Path,
				"size":         input.Size,
				"content_type": input.ContentType,
			},
		},
	}
	applyKnownFormatCapabilities(item)
	return item
}

func isGeoJSONResource(input SingleResourceInput) bool {
	return isGeoJSONPath(input.Name) ||
		isGeoJSONPath(input.Path) ||
		isGeoJSONContentType(input.ContentType)
}

func applyKnownFormatCapabilities(item *DetectedItem) {
	if item == nil {
		return
	}
	switch item.Format {
	case string(format.FormatJSON):
		if !isGeoJSONPath(item.PhysicalPath) &&
			!isGeoJSONPath(item.EntryPath) &&
			!isGeoJSONContentType(storageContentType(item)) {
			return
		}
		upsertNestedAttributeMap(item.Attributes, "capabilities", "spatial", map[string]interface{}{
			"geometry_columns": []map[string]interface{}{{
				"name":          "geometry",
				"geometry_type": "geometry",
				"srid":          4326,
			}},
			"primary_geometry_column": "geometry",
			"has_spatial_index":       false,
		})
	case string(format.FormatTIFF):
		upsertNestedAttributeMap(item.Attributes, "capabilities", "spatial", map[string]interface{}{
			"extent":            nil,
			"has_spatial_index": false,
		})
	}
}

func isGeoJSONPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".geojson")
}

func isGeoJSONContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	return contentType == "application/geo+json" || contentType == "application/vnd.geo+json"
}

func storageContentType(item *DetectedItem) string {
	if item == nil || item.Attributes == nil {
		return ""
	}
	storage, _ := item.Attributes["storage"].(map[string]interface{})
	contentType, _ := storage["content_type"].(string)
	return contentType
}

func upsertNestedAttributeMap(attrs map[string]interface{}, section string, namespace string, values map[string]interface{}) {
	if attrs == nil || section == "" || namespace == "" || len(values) == 0 {
		return
	}
	sectionAttrs, _ := attrs[section].(map[string]interface{})
	if sectionAttrs == nil {
		sectionAttrs = map[string]interface{}{}
	}
	namespaceAttrs, _ := sectionAttrs[namespace].(map[string]interface{})
	if namespaceAttrs == nil {
		namespaceAttrs = map[string]interface{}{}
	}
	for key, value := range values {
		namespaceAttrs[key] = value
	}
	sectionAttrs[namespace] = namespaceAttrs
	attrs[section] = sectionAttrs
}
