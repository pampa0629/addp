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

// ParseSchema 解析 Image Schema
// 对于图像文件，schema 只包含基本的元数据字段
func (p *Parser) ParseSchema(ctx context.Context, input io.Reader) (*format.Schema, error) {
	// 解码图像配置
	_, _, err := image.DecodeConfig(input)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image config: %w", err)
	}

	// 构建 schema（图像没有表格结构，但返回标准 schema 用于一致性）
	fields := []format.Field{
		{
			Name:     "width",
			Type:     format.FieldTypeInt,
			Nullable: false,
		},
		{
			Name:     "height",
			Type:     format.FieldTypeInt,
			Nullable: false,
		},
		{
			Name:     "format",
			Type:     format.FieldTypeString,
			Nullable: false,
		},
	}

	recordCount := int64(1) // 图像只有1条"记录"（自身）
	return &format.Schema{
		Fields:      fields,
		RecordCount: &recordCount,
	}, nil
}

// ReadRecords 读取 Image 数据记录（返回图像元数据作为单条记录）
func (p *Parser) ReadRecords(ctx context.Context, input io.Reader, offset, limit int64) ([]map[string]interface{}, error) {
	if offset > 0 {
		return []map[string]interface{}{}, nil
	}

	// 解码图像配置
	cfg, formatName, err := image.DecodeConfig(input)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	// 构建记录
	record := map[string]interface{}{
		"width":        cfg.Width,
		"height":       cfg.Height,
		"format":       formatName,
		"aspect_ratio": calculateAspectRatio(cfg.Width, cfg.Height),
		"resolution":   fmt.Sprintf("%dx%d", cfg.Width, cfg.Height),
		"megapixels":   float64(cfg.Width*cfg.Height) / 1000000.0,
	}

	return []map[string]interface{}{record}, nil
}

// CountRecords 统计总记录数（图像始终为1）
func (p *Parser) CountRecords(ctx context.Context, input io.Reader) (int64, error) {
	return 1, nil
}

// ExtractMetadata 提取 Image 元数据
func (p *Parser) ExtractMetadata(ctx context.Context, input io.Reader) (map[string]interface{}, error) {
	// 解码图像配置
	cfg, formatName, err := image.DecodeConfig(input)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	// 构建元数据
	metadata := map[string]interface{}{
		"width":       cfg.Width,
		"height":      cfg.Height,
		"format":      formatName,
		"color_model": inferColorModel(cfg),
		"aspect_ratio": calculateAspectRatio(cfg.Width, cfg.Height),
		"resolution":   fmt.Sprintf("%dx%d", cfg.Width, cfg.Height),
		"megapixels":   float64(cfg.Width*cfg.Height) / 1000000.0,
	}

	// 添加分类信息
	classification := classifyImage(cfg.Width, cfg.Height)
	for k, v := range classification {
		metadata[k] = v
	}

	return metadata, nil
}

// calculateAspectRatio 计算长宽比
func calculateAspectRatio(width, height int) string {
	if height == 0 {
		return "unknown"
	}

	ratio := float64(width) / float64(height)

	// 常见长宽比
	commonRatios := map[string]float64{
		"1:1":   1.0,
		"4:3":   4.0 / 3.0,
		"3:2":   3.0 / 2.0,
		"16:9":  16.0 / 9.0,
		"16:10": 16.0 / 10.0,
		"21:9":  21.0 / 9.0,
	}

	// 查找最接近的常见比例（误差在5%以内）
	for ratioStr, ratioVal := range commonRatios {
		if ratio >= ratioVal*0.95 && ratio <= ratioVal*1.05 {
			return ratioStr
		}
	}

	return fmt.Sprintf("%.2f:1", ratio)
}

// classifyImage 根据尺寸和比例分类图像
func classifyImage(width, height int) map[string]interface{} {
	classification := make(map[string]interface{})

	// 1. 按尺寸分类
	totalPixels := width * height
	if totalPixels < 100*100 {
		classification["size_category"] = "thumbnail"
	} else if totalPixels < 800*600 {
		classification["size_category"] = "small"
	} else if totalPixels < 1920*1080 {
		classification["size_category"] = "medium"
	} else if totalPixels < 3840*2160 {
		classification["size_category"] = "large"
	} else {
		classification["size_category"] = "very_large"
	}

	// 2. 按方向分类
	if width > height {
		classification["orientation"] = "landscape"
	} else if height > width {
		classification["orientation"] = "portrait"
	} else {
		classification["orientation"] = "square"
	}

	// 3. 判断是否可能是图标
	if width <= 512 && height <= 512 && width == height {
		classification["likely_icon"] = true
	}

	// 4. 判断是否可能是横幅/Banner
	ratio := float64(width) / float64(height)
	if ratio > 3.0 || ratio < 0.33 {
		classification["likely_banner"] = true
	}

	return classification
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

func init() {
	// 注册到全局 format registry
	_ = format.Register(NewParser(nil))
}
