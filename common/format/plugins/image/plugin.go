package image

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	_ "golang.org/x/image/tiff"
)

// Plugin 实现 Image 格式插件。
type Plugin struct {
	options    *format.ParseOptions
	formatType format.FormatType
}

// NewPlugin 创建 Image 插件。
func NewPlugin(opts *format.ParseOptions) *Plugin {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	return &Plugin{options: opts, formatType: format.FormatImage}
}

func NewProvider(formatType format.FormatType) *Plugin {
	return &Plugin{options: format.DefaultParseOptions(), formatType: formatType}
}

// inferColorModel 推断颜色模型
func inferColorModel(cfg image.Config) string {
	if cfg.ColorModel == nil {
		return "unknown"
	}

	// 简化实现：返回通用的颜色模型描述
	// Go 的 image.Config 不直接暴露颜色模型类型信息
	// 实际应用中可以通过解码完整图像来准确判断
	return "RGB"
}

func (p *Plugin) Format() format.FormatType {
	if p.formatType == "" {
		return format.FormatImage
	}
	return p.formatType
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	descriptor := format.FormatDescriptor{
		ID:            "builtin-" + string(p.Format()),
		Format:        p.Format(),
		I18nKey:       "format." + string(p.Format()),
		DataType:      format.FormatDataTypeMedia,
		Layouts:       []string{format.FormatLayoutSingle},
		ProviderHints: []string{format.FormatProviderMedia},
		Providers:     format.FormatProviderDescriptor{MediaInfo: true},
		ContentReaders: []string{
			string(format.ContentReaderRawContent),
			string(format.ContentReaderRangeContent),
		},
		EngineFamilies: []string{format.EngineFamilyObject, format.EngineFamilyFile},
	}
	switch p.Format() {
	case format.FormatJPEG:
		descriptor.Identification = format.FormatIdentification{Extensions: []string{".jpg", ".jpeg"}, MimeTypes: []string{"image/jpeg"}}
	case format.FormatPNG:
		descriptor.Identification = format.FormatIdentification{Extensions: []string{".png"}, MimeTypes: []string{"image/png"}}
	case format.FormatGIF:
		descriptor.Identification = format.FormatIdentification{Extensions: []string{".gif"}, MimeTypes: []string{"image/gif"}}
	case format.FormatTIFF:
		descriptor.Identification = format.FormatIdentification{Extensions: []string{".tif", ".tiff"}, MimeTypes: []string{"image/tiff"}}
	}
	return descriptor
}

func (p *Plugin) Capabilities() format.FormatCapability {
	capability, ok := format.GetFormatCapability(p.Format())
	if ok {
		return capability
	}
	return format.FormatCapability{
		Format:        p.Format(),
		DataType:      format.FormatDataTypeMedia,
		Layouts:       []string{format.FormatLayoutSingle},
		ProviderHints: []string{format.FormatProviderMedia},
	}
}

func (p *Plugin) DescribeMedia(ctx context.Context, input io.Reader, _ *format.ParseOptions) (*datatype.MediaDescribeResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg, formatName, data, err := decodeImageConfig(input, p.Format() == format.FormatTIFF)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info := &datatype.MediaInfo{
		Kind:       datatype.MediaKindImage,
		MIMEType:   imageMIMEType(formatName),
		Width:      cfg.Width,
		Height:     cfg.Height,
		Encoding:   formatName,
		ColorSpace: inferColorModel(cfg),
	}
	result := &datatype.MediaDescribeResult{Media: info}
	if len(data) > 0 {
		result.Spatial = extractGeoTIFFSpatial(data, cfg.Width, cfg.Height)
	}
	return result, nil
}

func decodeImageConfig(input io.Reader, keepData bool) (image.Config, string, []byte, error) {
	reader := input
	var data []byte
	if keepData {
		var err error
		data, err = io.ReadAll(input)
		if err != nil {
			return image.Config{}, "", nil, err
		}
		reader = bytes.NewReader(data)
	}
	cfg, formatName, err := image.DecodeConfig(reader)
	return cfg, formatName, data, err
}

func imageMIMEType(formatName string) string {
	switch strings.ToLower(strings.TrimSpace(formatName)) {
	case "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "tiff":
		return "image/tiff"
	default:
		if formatName == "" {
			return ""
		}
		return "image/" + strings.ToLower(formatName)
	}
}

func init() {
	for _, formatType := range []format.FormatType{
		format.FormatImage,
		format.FormatJPEG,
		format.FormatPNG,
		format.FormatGIF,
		format.FormatTIFF,
	} {
		_ = format.RegisterFormatPlugin(NewProvider(formatType))
	}
}
