package extractors

import (
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path/filepath"

	"github.com/addp/meta/internal/scanner"
)

// ImageExtractor 图像文件的元数据提取器
type ImageExtractor struct{}

func (e *ImageExtractor) SupportedTypes() []string {
	return []string{
		"image/*", // 通配符匹配所有图像类型
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

func (e *ImageExtractor) Priority() int {
	return 50 // 中等优先级
}

func (e *ImageExtractor) Extract(ctx context.Context, input scanner.ExtractInput) (*scanner.Metadata, error) {
	// 1. 解码图像以获取基本信息
	img, format, err := image.DecodeConfig(input.Reader)
	if err != nil {
		// 如果解码失败，返回基础元数据
		return e.extractBasicMetadata(input, "unknown", err), nil
	}

	// 2. 构建图像元数据
	imageMeta := &scanner.ImageMetadata{
		Width:      img.Width,
		Height:     img.Height,
		Format:     format,
		ColorSpace: inferColorSpace(img),
	}

	// 3. 构建基础元数据
	metadata := &scanner.Metadata{
		BasicInfo: scanner.BasicMetadata{
			FileName:     filepath.Base(input.ObjectKey),
			FileType:     fmt.Sprintf("Image (%s)", format),
			Size:         input.Size,
			ContentType:  input.ContentType,
			LastModified: input.LastModified,
			ETag:         input.ETag,
		},
		CustomAttrs: make(map[string]interface{}),
	}

	// 4. 添加图像专用元数据
	metadata.CustomAttrs["image_metadata"] = map[string]interface{}{
		"width":       imageMeta.Width,
		"height":      imageMeta.Height,
		"format":      imageMeta.Format,
		"color_space": imageMeta.ColorSpace,
		"aspect_ratio": calculateAspectRatio(imageMeta.Width, imageMeta.Height),
		"resolution":   fmt.Sprintf("%dx%d", imageMeta.Width, imageMeta.Height),
		"megapixels":   float64(imageMeta.Width*imageMeta.Height) / 1000000.0,
	}

	// 5. 推断图像用途
	metadata.CustomAttrs["image_classification"] = classifyImage(imageMeta)

	return metadata, nil
}

// extractBasicMetadata 提取基础元数据（当图像解码失败时）
func (e *ImageExtractor) extractBasicMetadata(input scanner.ExtractInput, format string, decodeErr error) *scanner.Metadata {
	metadata := &scanner.Metadata{
		BasicInfo: scanner.BasicMetadata{
			FileName:     filepath.Base(input.ObjectKey),
			FileType:     "Image",
			Size:         input.Size,
			ContentType:  input.ContentType,
			LastModified: input.LastModified,
			ETag:         input.ETag,
		},
		CustomAttrs: map[string]interface{}{
			"decode_error": decodeErr.Error(),
			"format":       format,
		},
	}

	return metadata
}

// inferColorSpace 推断色彩空间
func inferColorSpace(model image.Config) string {
	// 由于image.Config不直接暴露ColorModel，我们通过其他特征推断
	// 这里简化处理，返回通用值
	// 实际应用中可以通过解码图像并检查像素格式来准确判断
	return "RGB"
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
func classifyImage(meta *scanner.ImageMetadata) map[string]interface{} {
	classification := make(map[string]interface{})

	// 1. 按尺寸分类
	totalPixels := meta.Width * meta.Height
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
	if meta.Width > meta.Height {
		classification["orientation"] = "landscape"
	} else if meta.Height > meta.Width {
		classification["orientation"] = "portrait"
	} else {
		classification["orientation"] = "square"
	}

	// 3. 判断是否可能是图标
	if meta.Width <= 512 && meta.Height <= 512 && meta.Width == meta.Height {
		classification["likely_icon"] = true
	}

	// 4. 判断是否可能是横幅/Banner
	ratio := float64(meta.Width) / float64(meta.Height)
	if ratio > 3.0 || ratio < 0.33 {
		classification["likely_banner"] = true
	}

	return classification
}
