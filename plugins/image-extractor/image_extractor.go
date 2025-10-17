// Package imageextractor 图片文件元数据提取器插件
package imageextractor

import (
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path/filepath"

	sdk "github.com/addp/meta-extractor-sdk"
)

// init 函数：图片元数据类型已经在SDK中内置注册
func init() {
	// ImageMetadata已经在SDK的init()中注册
}

// ImageExtractor 图像文件的元数据提取器
type ImageExtractor struct{}

// SupportedTypes 返回支持的MIME类型
func (e *ImageExtractor) SupportedTypes() []string {
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

// Priority 返回优先级
func (e *ImageExtractor) Priority() int {
	return 50
}

// Extract 提取图像文件元数据
func (e *ImageExtractor) Extract(ctx context.Context, input sdk.ExtractInput) (*sdk.Metadata, error) {
	// 1. 解码图像获取配置信息
	imgConfig, format, err := image.DecodeConfig(input.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	// 2. 创建基础元数据
	metadata := sdk.NewMetadata(
		filepath.Base(input.ObjectKey),
		fmt.Sprintf("Image (%s)", format),
		input.Size,
	)

	metadata.BasicInfo.ContentType = input.ContentType
	metadata.BasicInfo.LastModified = input.LastModified
	metadata.BasicInfo.ETag = input.ETag

	// 3. 创建图像元数据
	imageMeta := &sdk.ImageMetadata{
		Width:      imgConfig.Width,
		Height:     imgConfig.Height,
		Format:     format,
		ColorSpace: inferColorSpace(imgConfig),
		BitDepth:   inferBitDepth(format),
		HasAlpha:   hasAlpha(format),
	}

	// 4. 添加类型化元数据
	metadata.AddTypedMetadata("image_metadata", imageMeta)

	// 5. 添加其他自定义属性
	metadata.CustomAttrs["file_size"] = input.Size // 文件字节大小
	metadata.CustomAttrs["file_size_human"] = formatFileSize(input.Size) // 人类可读格式
	metadata.CustomAttrs["resolution"] = fmt.Sprintf("%dx%d", imgConfig.Width, imgConfig.Height)
	metadata.CustomAttrs["aspect_ratio"] = calculateAspectRatio(imgConfig.Width, imgConfig.Height)
	metadata.CustomAttrs["megapixels"] = float64(imgConfig.Width*imgConfig.Height) / 1000000.0
	metadata.CustomAttrs["orientation"] = getOrientation(imgConfig.Width, imgConfig.Height)
	metadata.CustomAttrs["size_category"] = getSizeCategory(imgConfig.Width * imgConfig.Height)

	return metadata, nil
}

// formatFileSize 格式化文件大小为人类可读格式
func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

// inferColorSpace 推断色彩空间
func inferColorSpace(config image.Config) string {
	// 简化实现，默认返回RGB
	// 实际应用中可以通过ColorModel判断
	return "RGB"
}

// inferBitDepth 推断位深度
func inferBitDepth(format string) int {
	switch format {
	case "png":
		return 8 // PNG通常是8位或16位
	case "jpeg", "jpg":
		return 8
	case "gif":
		return 8
	default:
		return 8
	}
}

// hasAlpha 判断是否有Alpha通道
func hasAlpha(format string) bool {
	return format == "png" || format == "webp"
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

// getOrientation 获取图像方向
func getOrientation(width, height int) string {
	if width > height {
		return "landscape"
	} else if height > width {
		return "portrait"
	}
	return "square"
}

// getSizeCategory 根据像素数分类图像大小
func getSizeCategory(totalPixels int) string {
	if totalPixels < 100*100 {
		return "thumbnail"
	} else if totalPixels < 800*600 {
		return "small"
	} else if totalPixels < 1920*1080 {
		return "medium"
	} else if totalPixels < 3840*2160 {
		return "large"
	}
	return "very_large"
}

// GetExtractor 返回提取器实例（供ADDP加载使用）
func GetExtractor() sdk.MetadataExtractor {
	return &ImageExtractor{}
}
