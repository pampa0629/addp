package scanruntime

import (
	"context"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/scanresource"
)

func (s *ObjectStorageCatalogRuntime) detectObjectCatalogResourceFormats(
	ctx context.Context,
	readableProvider plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	resources []scanresource.StorageResource,
) {
	for i := range resources {
		if resources[i].NodeType != "object" || !needsContentFormatDetection(resources[i].Format) {
			continue
		}
		detected, err := detectObjectCatalogResourceFormat(ctx, readableProvider, connInfo, resources[i])
		if err != nil {
			if s.log != nil {
				s.log.Warn("对象内容格式嗅探失败，保留基础格式", "bucket", resources[i].RootName, "path", resources[i].Path, "error", err)
			}
			continue
		}
		if detected != "" {
			resources[i].Format = detected
		}
	}
}

func detectObjectCatalogResourceFormat(
	ctx context.Context,
	readableProvider plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	resource scanresource.StorageResource,
) (string, error) {
	if readableProvider == nil {
		return "", nil
	}
	detected, err := metaenrich.DetectSingleFileFormat(ctx, readableProvider, connInfo, resource.EngineCatalogPath, resource.Path)
	if err != nil {
		return "", err
	}
	if detected == format.FormatUnknown {
		return "", nil
	}
	return string(detected), nil
}

func needsContentFormatDetection(formatName string) bool {
	return format.NormalizeFormat(formatName) == format.FormatUnknown
}
