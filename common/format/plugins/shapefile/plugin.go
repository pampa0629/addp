package shapefile

import (
	"path/filepath"
	"strings"

	"github.com/addp/common/format"
	"github.com/addp/common/resource"
)

func (plugin *Plugin) Format() format.FormatType {
	return format.FormatShapefile
}

func (plugin *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:             "builtin-shapefile",
		Format:         format.FormatShapefile,
		I18nKey:        "format.shapefile",
		DataType:       format.FormatDataTypeTable,
		Layouts:        []string{format.FormatLayoutMulti},
		ProviderHints:  []string{format.FormatProviderTable, format.FormatProviderSpatial},
		Identification: format.FormatIdentification{Extensions: []string{".shp"}, MimeTypes: []string{"application/x-shapefile", "application/x-esri-shapefile"}},
		Providers:      format.FormatProviderDescriptor{FormatInfo: true, TableInfo: true, TableSample: true, Table: true, ComponentTable: true},
		ContentReaders: []string{string(format.ContentReaderTableSample), string(format.ContentReaderComponentTableSample), string(format.ContentReaderRawContent)},
		TransferRead:   true,
		TransferWrite:  true,
		Parse:          true,
		Spatial:        true,
		EngineFamilies: []string{format.EngineFamilyObject, format.EngineFamilyFile},
	}
}

func (plugin *Plugin) Capabilities() format.FormatCapability {
	capability, _ := format.GetFormatCapability(format.FormatShapefile)
	return capability
}

func (plugin *Plugin) ComponentSpecs() []resource.ComponentSpec {
	return ComponentSpecs()
}

func (plugin *Plugin) DescribeComponents(components []resource.ComponentRef) []format.ComponentDescriptor {
	return DescribeComponents(components)
}

func DescribeComponents(components []resource.ComponentRef) []format.ComponentDescriptor {
	descriptors := make([]format.ComponentDescriptor, 0, len(components))
	for _, component := range components {
		ext := strings.ToLower(filepath.Ext(component.Path))
		role := strings.TrimSpace(component.ComponentRole)
		if role == "" {
			role = roleForExtension(ext)
		}
		dataType, formatType := componentTypeForRole(role)
		descriptors = append(descriptors, format.ComponentDescriptor{
			Key:       role,
			Path:      component.Path,
			Role:      role,
			Label:     labelForRole(role, ext),
			Required:  component.Required,
			Primary:   component.Role == resource.ResourceRoleMain,
			DataType:  dataType,
			Format:    formatType,
			Extension: ext,
		})
	}
	return descriptors
}

func componentTypeForRole(role string) (string, format.FormatType) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "projection", "encoding":
		return format.FormatDataTypeDocument, format.FormatText
	default:
		return format.FormatDataTypeFile, format.FormatUnknown
	}
}

func roleForExtension(ext string) string {
	for _, spec := range ComponentSpecs() {
		if strings.EqualFold(resource.NormalizeExtension(spec.Extension), ext) {
			return spec.Role
		}
	}
	return strings.TrimPrefix(ext, ".")
}

func labelForRole(role, ext string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "main":
		return "主文件"
	case "index":
		return "索引文件"
	case "attributes":
		return "属性表"
	case "projection":
		return "坐标参考"
	case "encoding":
		return "编码声明"
	case "spatial_index":
		return "空间索引"
	default:
		if ext != "" {
			return strings.ToUpper(strings.TrimPrefix(ext, ".")) + " 文件"
		}
		return "组件文件"
	}
}
