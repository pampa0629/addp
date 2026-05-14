package shapefile

import (
	"context"
	"io"
	"path/filepath"
	"strings"

	"github.com/addp/common/format"
	"github.com/addp/common/resource"
)

type tableProvider struct {
	parser *Parser
}

func newTableProvider(parser *Parser) tableProvider {
	return tableProvider{parser: parser}
}

func (p tableProvider) Format() format.FormatType {
	return format.FormatShapefile
}

func (p tableProvider) Descriptor() format.FormatDescriptor {
	descriptor, ok := format.GetFormatDescriptor(format.FormatShapefile)
	if ok {
		return descriptor
	}
	return format.FormatDescriptor{
		ID:             "builtin-shapefile",
		Format:         format.FormatShapefile,
		DataType:       format.FormatDataTypeTable,
		Layouts:        []string{format.FormatLayoutMulti},
		ProviderHints:  []string{format.FormatProviderTable, format.FormatProviderSpatial},
		ContentReaders: []string{string(format.ContentReaderTableSample), string(format.ContentReaderComponentTableSample), string(format.ContentReaderRawContent)},
		Spatial:        true,
	}
}

func (p tableProvider) Capabilities() format.FormatCapability {
	capability, _ := format.GetFormatCapability(format.FormatShapefile)
	return capability
}

func (p tableProvider) ComponentSpecs() []resource.ComponentSpec {
	return ComponentSpecs()
}

func (p tableProvider) DescribeComponents(components []resource.ComponentRef) []format.ComponentDescriptor {
	return DescribeComponents(components)
}

func (p tableProvider) DescribeTable(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.TableInfo, error) {
	return p.parser.ParseTableInfo(ctx, input, options)
}

func (p tableProvider) SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	return p.parser.SampleTable(ctx, input, offset, limit, options)
}

func (p tableProvider) DescribeTableComponents(ctx context.Context, components resource.ComponentReader, options *format.ParseOptions) (*format.TableInfo, error) {
	return p.parser.DescribeTableComponents(ctx, components, options)
}

func (p tableProvider) SampleTableComponents(ctx context.Context, components resource.ComponentReader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	return p.parser.SampleTableComponents(ctx, components, offset, limit, options)
}

func DescribeComponents(components []resource.ComponentRef) []format.ComponentDescriptor {
	descriptors := make([]format.ComponentDescriptor, 0, len(components))
	for _, component := range components {
		ext := strings.ToLower(filepath.Ext(component.Path))
		role := strings.TrimSpace(component.ComponentRole)
		if role == "" {
			role = roleForExtension(ext)
		}
		preview := previewHintForComponent(component, role)
		descriptors = append(descriptors, format.ComponentDescriptor{
			Key:             role,
			Path:            component.Path,
			Role:            role,
			Label:           labelForRole(role, ext),
			Required:        component.Required,
			Primary:         component.Role == resource.ResourceRoleMain,
			DataType:        preview.DataType,
			Format:          preview.Format,
			Extension:       ext,
			PreviewDataType: preview.DataType,
			PreviewFormat:   preview.Format,
			PreviewMaterial: preview.Material,
			PreviewRenderer: preview.Renderer,
			Previewable:     &preview.Previewable,
		})
	}
	return descriptors
}

func previewHintForComponent(component resource.ComponentRef, role string) format.PreviewHint {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "projection", "encoding":
		return format.InferPreviewHint(format.PreviewHintInput{
			Name:     component.Name,
			Path:     component.Path,
			Format:   format.FormatText,
			DataType: format.FormatDataTypeDocument,
		})
	default:
		return format.PreviewHint{
			DataType:    format.FormatDataTypeFile,
			Format:      format.FormatUnknown,
			Material:    format.PreviewMaterialRawBinary,
			Renderer:    "text",
			Previewable: false,
		}
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
