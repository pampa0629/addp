package preview

import (
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/manager/internal/catalogutil"
)

func tableInfoFromMetaAttributes(attrs map[string]interface{}, fallbackName string) *datatype.TableInfo {
	return datatype.TableInfoFromPayload(commonJSON.Section(attrs, "type_info.table"), fallbackName)
}

func spatialInfoFromMetaAttributes(attrs map[string]interface{}) *datatype.SpatialInfo {
	return datatype.SpatialInfoFromPayload(commonJSON.Section(attrs, "capabilities.spatial"))
}

func graphInfoFromMetaAttributes(attrs map[string]interface{}) *datatype.GraphInfo {
	return datatype.GraphInfoFromPayload(commonJSON.Section(attrs, "type_info.graph"))
}

func itemDataTypeFromMetaAttributes(attrs map[string]interface{}) string {
	return strings.ToLower(strings.TrimSpace(catalogutil.StringAttribute(attrs, "data_type")))
}

func itemLayoutFromMetaAttributes(attrs map[string]interface{}) string {
	return strings.ToLower(strings.TrimSpace(catalogutil.StringAttribute(attrs, "layout")))
}

func physicalPathFromMetaAttributes(attrs map[string]interface{}) string {
	return strings.TrimSpace(catalogutil.StringAttribute(attrs, "physical_path"))
}

func formatNameFromMetaAttributes(attrs map[string]interface{}) string {
	return strings.TrimSpace(catalogutil.StringAttribute(attrs, "format"))
}

func formatTypeFromMetaAttributes(attrs map[string]interface{}) format.FormatType {
	formatName := formatNameFromMetaAttributes(attrs)
	if formatName == "" {
		return format.FormatUnknown
	}
	return format.NormalizeFormat(formatName)
}

func fileFormatTypeFromMetaAttributes(attrs map[string]interface{}) format.FormatType {
	if formatType := formatTypeFromMetaAttributes(attrs); formatType != "" && formatType != format.FormatUnknown {
		return formatType
	}
	return format.MIMEToFormat(catalogutil.StringAttribute(attrs, "content_type"))
}
