package scanruntime

import (
	"context"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/scanresource"
)

func (s *FilesystemCatalogRuntime) listDirectory(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.EngineCatalogProvider,
	connInfo plugin.ConnectionInfo,
	dirPath string,
) ([]metaitem.StorageFileRef, []metaitem.StorageDirectoryRef, error) {
	files, subdirs, _, err := s.listDirectoryWithIgnored(ctx, resource, catalogProvider, connInfo, dirPath)
	return files, subdirs, err
}

func (s *FilesystemCatalogRuntime) listDirectoryWithIgnored(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.EngineCatalogProvider,
	connInfo plugin.ConnectionInfo,
	dirPath string,
) ([]metaitem.StorageFileRef, []metaitem.StorageDirectoryRef, []plugin.EngineCatalogEntry, error) {
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
	catalogProvider plugin.EngineCatalogProvider,
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

func splitFileCatalogEntries(nodes []plugin.EngineCatalogEntry) ([]metaitem.StorageFileRef, []metaitem.StorageDirectoryRef, []plugin.EngineCatalogEntry) {
	files := make([]metaitem.StorageFileRef, 0, len(nodes))
	subdirs := make([]metaitem.StorageDirectoryRef, 0, len(nodes))
	ignored := make([]plugin.EngineCatalogEntry, 0)
	for _, node := range nodes {
		if scanresource.IgnoreSystemEngineCatalogEntry(node) {
			ignored = append(ignored, node)
			continue
		}
		if node.Role == plugin.EngineCatalogRoleBranch {
			if dir, ok := scanresource.StorageDirectoryRefFromEntry(node); ok {
				subdirs = append(subdirs, dir)
			}
			continue
		}
		if file, ok := scanresource.StorageFileRefFromEntry(node); ok {
			files = append(files, file)
		}
	}
	return files, subdirs, ignored
}
