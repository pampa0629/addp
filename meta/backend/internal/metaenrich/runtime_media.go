package metaenrich

import (
	"context"
	"fmt"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

type RuntimeMediaInspector interface {
	InspectMedia(context.Context, *commonModels.Engine, uint, string, string) (*format.MediaDescribeResult, error)
}

func EnrichRuntimeMediaItem(ctx context.Context, attrs models.JSONMap, inspector RuntimeMediaInspector, source *commonModels.Engine, tenantID uint, item *metaitem.DetectedItem, path string) (bool, error) {
	if item == nil || item.DataType != datatype.Media {
		return false, nil
	}
	formatType := format.NormalizeFormat(item.Format)
	if _, err := format.GetRuntimeMediaInfoProviderFactory(formatType); err != nil {
		return false, nil
	}
	// Clear the in-flight description before inspecting this source version.
	metaattr.ReplaceAttributeNamespace(attrs, "type_info", "media", nil)
	metaattr.ReplaceCapabilityNamespace(attrs, "spatial", nil)
	metaattr.ReplaceAttributeNamespace(attrs, "format_info", string(formatType), nil)
	item.Media = nil
	if inspector == nil {
		return true, fmt.Errorf("runtime media deep scan requires a configured inspector")
	}
	info, err := inspector.InspectMedia(ctx, source, tenantID, path, string(formatType))
	if err != nil {
		return true, err
	}
	if info == nil || info.Media == nil {
		return true, fmt.Errorf("runtime media inspector returned no media facts")
	}
	item.Media = info.Media.Clone()
	mediaAttrs := metaattr.MediaInfoAttributes(info.Media, info.Spatial)
	metaattr.ReplaceAttributeNamespace(attrs, "type_info", "media", commonJSON.Section(mediaAttrs, "type_info.media"))
	metaattr.ReplaceCapabilityNamespace(attrs, "spatial", commonJSON.Section(mediaAttrs, "capabilities.spatial"))
	formatAttrs := metaattr.FormatInfoAttributes(string(formatType), info.FormatInfo)
	metaattr.ReplaceAttributeNamespace(attrs, "format_info", string(formatType), commonJSON.Section(formatAttrs, "format_info."+string(formatType)))
	return true, nil
}
