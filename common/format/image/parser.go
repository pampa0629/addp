package image

import (
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"

	"github.com/addp/common/format"
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

// ============ ObjectInfoParser 接口实现 ============

// ParseObjectInfo 实现 ObjectInfoParser 接口
// 从图像文件中提取 ObjectInfo（包含 ImageInfo 扩展信息）
func (p *Parser) ParseObjectInfo(ctx context.Context, input io.Reader, basicInfo format.ObjectBasicInfo) (*format.ObjectInfo, error) {
	// 解码图像配置
	cfg, formatName, err := image.DecodeConfig(input)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	// 构建 ImageInfo
	imageInfo := &format.ImageInfo{
		Width:      cfg.Width,
		Height:     cfg.Height,
		Format:     formatName,
		ColorSpace: inferColorModel(cfg),
	}

	// 构建 ObjectInfo
	objectInfo := &format.ObjectInfo{
		Key:         basicInfo.Key,
		SizeBytes:   basicInfo.SizeBytes,
		ModifiedAt:  basicInfo.ModifiedAt,
		ContentType: basicInfo.ContentType,
		ETag:        basicInfo.ETag,
		Extensions:  []format.ExtensionInfo{imageInfo},
	}

	return objectInfo, nil
}

// SupportedContentTypes 实现 ObjectInfoParser 接口
// 返回支持的 MIME 类型
func (p *Parser) SupportedContentTypes() []string {
	return []string{
		"image/*",       // 通配符
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

func init() {
	parser := NewParser(nil)
	// 注册为 ObjectInfoParser
	_ = format.RegisterObjectInfoParser(parser)
}
