package shapefile

import (
	"path/filepath"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
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
		Providers:      format.FormatProviderDescriptor{FormatInfo: true, TableInfo: true, TableSample: true, Table: true, MultiTable: true},
		ContentReaders: []string{string(format.ContentReaderMultiTableSample), string(format.ContentReaderRawContent)},
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

func (plugin *Plugin) RelatedRefSpecs() []contentio.RelatedRefSpec {
	return RelatedRefSpecs()
}

func (plugin *Plugin) DescribeRefs(refs []contentio.Ref) []format.RefDescriptor {
	return DescribeRefs(refs)
}

func DescribeRefs(refs []contentio.Ref) []format.RefDescriptor {
	descriptors := make([]format.RefDescriptor, 0, len(refs))
	for _, ref := range refs {
		ext := strings.ToLower(filepath.Ext(ref.Path))
		role := strings.TrimSpace(ref.Role)
		if role == "" {
			role = roleForExtension(ext)
		}
		dataType, formatType := refTypeForRole(role)
		descriptors = append(descriptors, format.RefDescriptor{
			Key:       role,
			Path:      ref.Path,
			Role:      role,
			Label:     labelForRole(role, ext),
			Required:  ref.Required,
			Primary:   ref.Role == contentio.RoleMain,
			DataType:  dataType,
			Format:    formatType,
			Extension: ext,
		})
	}
	return descriptors
}

func refTypeForRole(role string) (string, format.FormatType) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "projection", "encoding":
		return format.FormatDataTypeDocument, format.FormatText
	default:
		return format.FormatDataTypeFile, format.FormatUnknown
	}
}

func roleForExtension(ext string) string {
	for _, spec := range RelatedRefSpecs() {
		if strings.EqualFold(contentio.NormalizeExtension(spec.Extension), ext) {
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
		return "相关文件"
	}
}
