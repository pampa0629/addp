package metaattr

import (
	"github.com/addp/common/datatype"
	"github.com/addp/meta/internal/models"
)

func MediaInfoAttributes(mediaInfo *datatype.MediaInfo, spatialInfo *datatype.SpatialInfo) models.JSONMap {
	attrs := models.JSONMap{}
	if mediaInfo == nil {
		return attrs
	}
	media := map[string]interface{}{}
	if mediaInfo.Kind != "" {
		media["kind"] = string(mediaInfo.Kind)
	}
	if mediaInfo.Width > 0 {
		media["width"] = mediaInfo.Width
	}
	if mediaInfo.Height > 0 {
		media["height"] = mediaInfo.Height
	}
	if mediaInfo.DurationMS != nil {
		media["duration_ms"] = *mediaInfo.DurationMS
	}
	if mediaInfo.Encoding != "" {
		media["encoding"] = mediaInfo.Encoding
	}
	if mediaInfo.ColorSpace != "" {
		media["color_space"] = mediaInfo.ColorSpace
	}
	if mediaInfo.MIMEType != "" {
		media["mime_type"] = mediaInfo.MIMEType
	}
	if mediaInfo.SizeBytes != nil {
		media["size_bytes"] = *mediaInfo.SizeBytes
	}
	if len(media) > 0 {
		UpsertNested(attrs, "type_info", "media", media)
	}
	if spatialAttrs := SpatialInfoAttributes(spatialInfo); len(spatialAttrs) > 0 {
		UpsertNested(attrs, "capabilities", "spatial", spatialAttrs)
	}
	return attrs
}
