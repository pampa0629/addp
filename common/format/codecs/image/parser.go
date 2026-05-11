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
	"path/filepath"
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

func NewProvider(formatType format.FormatType) format.MediaProvider {
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

// Extract 实现 FileMetadataExtractor 接口。
func (p *Parser) Extract(ctx context.Context, input format.ExtractInput) (*format.ExtractedMetadata, error) {
	cfg, formatName, data, err := decodeImageConfig(input.Reader, isTIFFInput(input))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	customAttrs := map[string]interface{}{
		"kind":        "image",
		"width":       cfg.Width,
		"height":      cfg.Height,
		"format":      formatName,
		"color_space": inferColorModel(cfg),
	}
	if len(data) > 0 {
		if spatial := extractGeoTIFFSpatial(data, cfg.Width, cfg.Height); len(spatial) > 0 {
			customAttrs["spatial"] = spatial
		}
	}

	return &format.ExtractedMetadata{
		BasicInfo: format.BasicMetadata{
			FileType:     "Image",
			Size:         input.Size,
			ContentType:  input.ContentType,
			LastModified: input.LastModified,
			ETag:         input.ETag,
		},
		CustomAttrs: customAttrs,
	}, nil
}

func (p *Parser) Format() format.FormatType {
	if p.formatType == "" {
		return format.FormatImage
	}
	return p.formatType
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
		Preview:       true,
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
		Format:      p.Format(),
		MediaType:   "image",
		MIMEType:    imageMIMEType(formatName),
		Width:       cfg.Width,
		Height:      cfg.Height,
		Encoding:    formatName,
		ColorSpace:  inferColorModel(cfg),
		PreviewKind: "image",
	}
	if len(data) > 0 {
		info.SpatialAttrs = extractGeoTIFFSpatial(data, cfg.Width, cfg.Height)
	}
	return info, nil
}

// SupportedTypes 实现 FileMetadataExtractor 接口。
func (p *Parser) SupportedTypes() []string {
	return []string{
		"image/*",
		"image/jpeg",
		"image/jpg",
		"image/png",
		"image/gif",
		"image/bmp",
		"image/webp",
		"image/tiff",
		"image/svg+xml",
	}
}

// Priority 实现 FileMetadataExtractor 接口。
func (p *Parser) Priority() int {
	return 100
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

func isTIFFInput(input format.ExtractInput) bool {
	contentType := strings.ToLower(strings.TrimSpace(input.ContentType))
	if contentType == "image/tiff" || contentType == "image/tif" {
		return true
	}
	ext := strings.ToLower(filepath.Ext(input.ObjectKey))
	return ext == ".tif" || ext == ".tiff"
}

func init() {
	parser := NewParser(nil)
	_ = format.RegisterExtractor(parser)
	for _, formatType := range []format.FormatType{
		format.FormatImage,
		format.FormatJPEG,
		format.FormatPNG,
		format.FormatGIF,
		format.FormatTIFF,
	} {
		_ = format.RegisterMediaProvider(NewProvider(formatType))
	}
}
