package scanruntime

import (
	"context"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/scanflow"
)

func (s *FilesystemCatalogRuntime) resolveFileCatalogDirectoryItems(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	catalogProvider plugin.EngineCatalogProvider,
	connInfo plugin.ConnectionInfo,
	resource *commonModels.Engine,
	dirPath string,
	files []metaitem.StorageFileRef,
	subdirs []metaitem.StorageDirectoryRef,
) (*metaitem.DetectionResult, error) {
	var recursiveFiles []metaitem.StorageFileRef
	var recursiveSubdirs []metaitem.StorageDirectoryRef
	if len(subdirs) > 0 {
		var err error
		recursiveFiles, recursiveSubdirs, err = s.listDirectoryRecursive(ctx, resource, catalogProvider, connInfo, dirPath)
		if err != nil {
			return nil, err
		}
	}

	return scanflow.DetectFileCatalogDirectoryItems(ctx, contentReader, connInfo, resource.ID, dirPath, files, subdirs, recursiveFiles, recursiveSubdirs)
}

func resolveNonExclusiveScopeItems(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	resource *commonModels.Engine,
	dirPath string,
	files []metaitem.StorageFileRef,
	subdirs []metaitem.StorageDirectoryRef,
) (*metaitem.DetectionResult, error) {
	return scanflow.DetectFileCatalogNonExclusiveScopeItems(ctx, contentReader, connInfo, resource.ID, dirPath, files, subdirs)
}
