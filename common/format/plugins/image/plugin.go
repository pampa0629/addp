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

	"github.com/addp/common/format"
	_ "golang.org/x/image/tiff"
)

// Parser 实现 Image 格式的解析器
type Parser struct {
	options    *format.ParseOptions
	formatType format.FormatType
}

// NewParser 创建 Image 解析器
func NewParser(opts *format.ParseOptions) *Parser {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	return &Parser{options: opts, formatType: format.FormatImage}
}

func NewProvider(formatType format.FormatType) *Parser {
	return &Parser{options: format.DefaultParseOptions(), formatType: formatType}
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

func (p *Parser) Format() format.FormatType {
	if p.formatType == "" {
		return format.FormatImage
	}
	return p.formatType
}

func (p *Parser) Descriptor() format.FormatDescriptor {
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

func (p *Parser) Capabilities() format.FormatCapability {
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

func (p *Parser) DescribeMedia(ctx context.Context, input io.Reader, _ *format.ParseOptions) (*format.MediaInfo, error) {
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
	info := &format.MediaInfo{
		Format:     p.Format(),
		MediaType:  "image",
		MIMEType:   imageMIMEType(formatName),
		Width:      cfg.Width,
		Height:     cfg.Height,
		Encoding:   formatName,
		ColorSpace: inferColorModel(cfg),
	}
	if len(data) > 0 {
		info.SpatialAttrs = extractGeoTIFFSpatial(data, cfg.Width, cfg.Height)
	}
	return info, nil
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
