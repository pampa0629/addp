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
	options *format.ParseOptions
}

// NewParser 创建 Image 解析器
func NewParser(opts *format.ParseOptions) *Parser {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	return &Parser{options: opts}
}

// SupportedFormats 返回支持的格式
func (p *Parser) SupportedFormats() []format.FormatType {
	return []format.FormatType{
		format.FormatImage,
		format.FormatJPEG,
		format.FormatPNG,
		format.FormatGIF,
		format.FormatTIFF,
	}
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
	reader := input.Reader
	var data []byte
	if isTIFFInput(input) {
		var err error
		data, err = io.ReadAll(input.Reader)
		if err != nil {
			return nil, fmt.Errorf("failed to read image: %w", err)
		}
		reader = bytes.NewReader(data)
	}

	cfg, formatName, err := image.DecodeConfig(reader)
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
}
