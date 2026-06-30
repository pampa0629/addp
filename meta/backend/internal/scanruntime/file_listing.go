package scanruntime

import (
	"context"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metaitem"
)

func (s *FilesystemCatalogRuntime) listDirectory(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.CatalogProvider,
	connInfo plugin.ConnectionInfo,
	dirPath string,
) ([]metaitem.StorageFileRef, []metaitem.StorageDirectoryRef, error) {
	files, subdirs, _, err := s.listDirectoryWithIgnored(ctx, resource, catalogProvider, connInfo, dirPath)
	return files, subdirs, err
}

func (s *FilesystemCatalogRuntime) listDirectoryWithIgnored(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.CatalogProvider,
	connInfo plugin.ConnectionInfo,
	dirPath string,
) ([]metaitem.StorageFileRef, []metaitem.StorageDirectoryRef, []plugin.CatalogEntry, error) {
	nodes, err := catalogProvider.ListChildren(ctx, connInfo, plugin.FileDirectoryPath(resource.ID, dirPath), plugin.ListOptions{})
	if err != nil {
		return nil, nil, nil, err
	}
	files, subdirs, ignored := splitFileCatalogEntries(nodes)
	return files, subdirs, ignored, nil
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
	files, subdirs, _ := splitFileCatalogEntries(nodes)
	return files, subdirs, nil
}

func splitFileCatalogEntries(nodes []plugin.CatalogEntry) ([]metaitem.StorageFileRef, []metaitem.StorageDirectoryRef, []plugin.CatalogEntry) {
	files := make([]metaitem.StorageFileRef, 0, len(nodes))
	subdirs := make([]metaitem.StorageDirectoryRef, 0, len(nodes))
	ignored := make([]plugin.CatalogEntry, 0)
	for _, node := range nodes {
		if metacatalog.IgnoreSystemCatalogEntry(node) {
			ignored = append(ignored, node)
			continue
		}
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
	return files, subdirs, ignored
}
