package extractors

import (
	"github.com/addp/common/format"
	"context"
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path/filepath"

	imageParser "github.com/addp/common/format/image"
)

// ImageMetadata 图像元数据（本地定义）
type ImageMetadata struct {
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Format     string `json:"format"`
	ColorSpace string `json:"color_space"`
}

// ImageExtractor 图像文件的元数据提取器
// 使用共享的 image parser 避免重复代码
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

func (e *ImageExtractor) Extract(ctx context.Context, input format.ExtractInput) (*format.ExtractedMetadata, error) {
	// 1. 创建共享 image parser
	parser := imageParser.NewParser(nil)

	// 2. 构建 ObjectBasicInfo
	basicInfo := format.ObjectBasicInfo{
		Key:         input.ObjectKey,
		SizeBytes:   input.Size,
		ContentType: input.ContentType,
		ETag:        input.ETag,
		ModifiedAt:  input.LastModified,
	}

	// 3. 使用新接口提取 ObjectInfo
	objectInfo, err := parser.ParseObjectInfo(ctx, input.Reader, basicInfo)
	if err != nil {
		// 如果解码失败，返回基础元数据
		return e.extractBasicMetadata(input, "unknown", err), nil
	}

	// 4. 从 ObjectInfo 中提取 ImageInfo
	var imageInfo *format.ImageInfo
	for _, ext := range objectInfo.Extensions {
		if ext.ExtensionType() == "image" {
			if img, ok := ext.(*format.ImageInfo); ok {
				imageInfo = img
				break
			}
		}
	}

	if imageInfo == nil {
		return e.extractBasicMetadata(input, "unknown", fmt.Errorf("no image info in object")), nil
	}

	// 5. 构建基础元数据
	metadata := &format.ExtractedMetadata{
		BasicInfo: format.BasicMetadata{
			FileName:     filepath.Base(input.ObjectKey),
			FileType:     fmt.Sprintf("Image (%s)", imageInfo.Format),
			Size:         input.Size,
			ContentType:  input.ContentType,
			LastModified: input.LastModified,
			ETag:         input.ETag,
		},
		CustomAttrs: make(map[string]interface{}),
	}

	// 6. 添加图像专用元数据
	metadata.CustomAttrs["image_metadata"] = map[string]interface{}{
		"width":       imageInfo.Width,
		"height":      imageInfo.Height,
		"format":      imageInfo.Format,
		"color_model": imageInfo.ColorSpace,
	}

	// 7. 构建 ImageMetadata 结构（兼容旧代码）
	imageMeta := &ImageMetadata{
		Width:      imageInfo.Width,
		Height:     imageInfo.Height,
		Format:     imageInfo.Format,
		ColorSpace: imageInfo.ColorSpace,
	}
	metadata.CustomAttrs["image_classification"] = classifyImage(imageMeta)

	return metadata, nil
}

// extractBasicMetadata 提取基础元数据（当图像解码失败时）
func (e *ImageExtractor) extractBasicMetadata(input format.ExtractInput, imageFormat string, decodeErr error) *format.ExtractedMetadata {
	metadata := &format.ExtractedMetadata{
		BasicInfo: format.BasicMetadata{
			FileName:     filepath.Base(input.ObjectKey),
			FileType:     "Image",
			Size:         input.Size,
			ContentType:  input.ContentType,
			LastModified: input.LastModified,
			ETag:         input.ETag,
		},
		CustomAttrs: map[string]interface{}{
			"decode_error": decodeErr.Error(),
			"format": imageFormat,
		},
	}

	return metadata
}

// classifyImage 根据尺寸和比例分类图像
func classifyImage(meta *ImageMetadata) map[string]interface{} {
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
