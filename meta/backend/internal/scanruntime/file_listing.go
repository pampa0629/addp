package scanruntime

import (
	"context"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metaitem"
)

func (s *FilesystemCatalogRuntime) ListDirectory(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.CatalogProvider,
	connInfo plugin.ConnectionInfo,
	dirPath string,
) ([]metaitem.StorageFileRef, []metaitem.StorageDirectoryRef, error) {
	nodes, err := catalogProvider.ListChildren(ctx, connInfo, plugin.FileDirectoryPath(resource.ID, dirPath), plugin.ListOptions{})
	if err != nil {
		return nil, nil, err
	}
	files, subdirs := splitFileCatalogEntries(nodes)
	return files, subdirs, nil
}

func (s *FilesystemCatalogRuntime) listDirectoryRecursive(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.CatalogProvider,
	connInfo plugin.ConnectionInfo,
	dirPath string,
) ([]metaitem.StorageFileRef, []metaitem.StorageDirectoryRef, error) {
	nodes, err := catalogProvider.ListChildren(ctx, connInfo, plugin.FileDirectoryPath(resource.ID, dirPath), plugin.ListOptions{Recursive: true})
	if err != nil {
		return nil, nil, err
	}
	files, subdirs := splitFileCatalogEntries(nodes)
	return files, subdirs, nil
}

func splitFileCatalogEntries(nodes []plugin.CatalogEntry) ([]metaitem.StorageFileRef, []metaitem.StorageDirectoryRef) {
	files := make([]metaitem.StorageFileRef, 0, len(nodes))
	subdirs := make([]metaitem.StorageDirectoryRef, 0, len(nodes))
	for _, node := range nodes {
		if node.Role == plugin.CatalogRoleBranch {
			if dir, ok := metacatalog.StorageDirectoryRefFromEntry(node); ok {
				subdirs = append(subdirs, dir)
			}
			continue
		}
		if file, ok := metacatalog.StorageFileRefFromEntry(node); ok {
			files = append(files, file)
		}
	}
	return files, subdirs
}
