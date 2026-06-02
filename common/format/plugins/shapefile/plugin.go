package shapefile

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Plugin 实现 Shapefile 格式插件
type Plugin struct {
	options *format.ParseOptions
}

// NewPlugin 创建 Shapefile 插件
func NewPlugin(opts *format.ParseOptions) *Plugin {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	return &Plugin{options: opts}
}

func (plugin *Plugin) Format() format.FormatType {
	return format.FormatShapefile
}

func (plugin *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:             "builtin-shapefile",
		Format:         format.FormatShapefile,
		I18nKey:        "format.shapefile",
		DataType:       datatype.DataTypeTable,
		Layouts:        []string{format.LayoutMulti},
		ProviderHints:  []string{format.FormatProviderTable, format.FormatProviderSpatial},
		Identification: format.FormatIdentification{Extensions: []string{extSHP}, MimeTypes: []string{"application/x-shapefile", "application/x-esri-shapefile"}},
		Providers:      format.FormatProviderDescriptor{MultiTable: true},
		ContentReaders: []string{string(format.ContentReaderMultiTableSample), string(format.ContentReaderRawContent)},
		Parse:          true,
		Spatial:        true,
	}
}

func (plugin *Plugin) RelatedRefSpecs() []format.RelatedRefSpec {
	return RelatedRefSpecs()
}

func (plugin *Plugin) DescribeRefs(refs []format.RelatedRef) []format.RefDescriptor {
	return DescribeRefs(refs)
}

func init() {
	_ = format.RegisterFormatPlugin(NewPlugin(nil))
}

const (
	extSHP = ".shp"
	extSHX = ".shx"
	extDBF = ".dbf"
	extPRJ = ".prj"
	extCPG = ".cpg"
	extSBN = ".sbn"
	extSBX = ".sbx"

	roleMain         = "main"
	roleIndex        = "index"
	roleAttributes   = "attributes"
	roleProjection   = "projection"
	roleEncoding     = "encoding"
	roleSpatialIndex = "spatial_index"
)

func RelatedRefSpecs() []format.RelatedRefSpec {
	return []format.RelatedRefSpec{
		{Extension: extSHP, Role: roleMain, Required: true, Primary: true},
		{Extension: extSHX, Role: roleIndex, Required: true},
		{Extension: extDBF, Role: roleAttributes, Required: true},
		{Extension: extPRJ, Role: roleProjection, Required: false},
		{Extension: extCPG, Role: roleEncoding, Required: false},
		{Extension: extSBN, Role: roleSpatialIndex, Required: false},
		{Extension: extSBX, Role: roleSpatialIndex, Required: false},
	}
}

func refExtensions(refs []format.RelatedRef) []string {
	seen := map[string]bool{}
	extensions := make([]string, 0, len(refs))
	for _, ref := range refs {
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(ref.Ref.Path)), ".")
		if ext == "" || seen[ext] {
			continue
		}
		seen[ext] = true
		extensions = append(extensions, ext)
	}
	sort.Strings(extensions)
	return extensions
}

func DescribeRefs(refs []format.RelatedRef) []format.RefDescriptor {
	descriptors := make([]format.RefDescriptor, 0, len(refs))
	for _, ref := range refs {
		ext := strings.ToLower(filepath.Ext(ref.Ref.Path))
		role := strings.TrimSpace(ref.Ref.Role)
		if role == "" {
			role = roleForExtension(ext)
		}
		dataType, formatType := refTypeForRole(role)
		descriptors = append(descriptors, format.RefDescriptor{
			Key:       role,
			Path:      ref.Ref.Path,
			Role:      role,
			Label:     labelForRole(role, ext),
			Required:  ref.Required,
			Primary:   ref.Primary,
			DataType:  dataType,
			Format:    formatType,
			Extension: ext,
		})
	}
	return descriptors
}

func refTypeForRole(role string) (datatype.DataType, format.FormatType) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case roleProjection, roleEncoding:
		return datatype.DataTypeDocument, format.FormatText
	default:
		return datatype.DataTypeUnknown, format.FormatUnknown
	}
}

func roleForExtension(ext string) string {
	for _, spec := range RelatedRefSpecs() {
		if strings.EqualFold(format.NormalizeExtension(spec.Extension), ext) {
			return spec.Role
		}
	}
	return strings.TrimPrefix(ext, ".")
}

func labelForRole(role, ext string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case roleMain:
		return "主文件"
	case roleIndex:
		return "索引文件"
	case roleAttributes:
		return "属性表"
	case roleProjection:
		return "坐标参考"
	case roleEncoding:
		return "编码声明"
	case roleSpatialIndex:
		return "空间索引"
	default:
		if ext != "" {
			return strings.ToUpper(strings.TrimPrefix(ext, ".")) + " 内容"
		}
		return "相关内容"
	}
}

func (plugin *Plugin) resolveOptions(options *format.ParseOptions) *format.ParseOptions {
	if options != nil {
		copied := *options
		return &copied
	}
	if plugin != nil && plugin.options != nil {
		copied := *plugin.options
		return &copied
	}
	return format.DefaultParseOptions()
}

func (plugin *Plugin) resolveMaterializedOptions(basePath string, options *format.ParseOptions) *format.ParseOptions {
	opts := plugin.resolveOptions(options)
	applyMaterializedSidecarOptions(basePath, opts)
	return opts
}

func applyMaterializedSidecarOptions(basePath string, opts *format.ParseOptions) {
	if opts == nil {
		return
	}
	if opts.SpatialRefSys == "" {
		if prjBytes, readErr := os.ReadFile(basePath + extPRJ); readErr == nil {
			opts.SpatialRefSys = strings.TrimSpace(string(prjBytes))
		}
	}
	if opts.Encoding == "" || NormalizeDBFEncoding(opts.Encoding) == "utf-8" {
		if cpgEncoding := readCPGEncoding(basePath); cpgEncoding != "" {
			opts.Encoding = cpgEncoding
		}
	}
}

func (plugin *Plugin) getGeometryFieldName(options *format.ParseOptions) string {
	if name := geometryFieldNameFromOptions(options); name != "" {
		return name
	}
	if plugin != nil {
		if name := geometryFieldNameFromOptions(plugin.options); name != "" {
			return name
		}
	}
	return "geometry"
}

func geometryFieldNameFromOptions(options *format.ParseOptions) string {
	if options == nil || options.ExtraParams == nil {
		return ""
	}
	name, ok := options.ExtraParams["geometry_field"].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(name)
}

type Info struct {
	BaseName      string   `json:"base_name,omitempty"`
	RefExtensions []string `json:"ref_extensions,omitempty"`
	HasPRJ        bool     `json:"has_prj,omitempty"`
	HasCPG        bool     `json:"has_cpg,omitempty"`
	ShapeType     string   `json:"shape_type,omitempty"`
	DBFVersion    byte     `json:"dbf_version,omitempty"`
	Encoding      string   `json:"encoding,omitempty"`
}

func (i *Info) FormatAttributes() map[string]interface{} {
	if i == nil {
		return nil
	}
	attrs := map[string]interface{}{}
	if i.BaseName != "" {
		attrs["base_name"] = i.BaseName
	}
	if len(i.RefExtensions) > 0 {
		attrs["ref_extensions"] = append([]string(nil), i.RefExtensions...)
	}
	attrs["has_prj"] = i.HasPRJ
	attrs["has_cpg"] = i.HasCPG
	return attrs
}

var tableNativeKeys = datatype.NewNativeAllowedKeys("shape_type", "dbf_version", "encoding")

func (i *Info) TableNative() map[string]interface{} {
	if i == nil {
		return nil
	}
	native := map[string]interface{}{}
	if i.ShapeType != "" {
		native["shape_type"] = i.ShapeType
	}
	if i.DBFVersion != 0 {
		native["dbf_version"] = i.DBFVersion
	}
	if i.Encoding != "" {
		native["encoding"] = i.Encoding
	}
	return datatype.FilterTableNative(native, tableNativeKeys)
}
