package scanruntime

import (
	"context"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/scanresource"
)

type objectCatalogPathTarget struct {
	Bucket string
	Prefix string
	Object string
}

func listObjectCatalogBucketNodes(ctx context.Context, resource *commonModels.Engine, catalogProvider plugin.EngineCatalogProvider) ([]plugin.EngineCatalogEntry, error) {
	nodes, err := catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.ObjectRootPath(resource.ID), plugin.ListOptions{})
	if err != nil {
		return nil, err
	}

	buckets := make([]plugin.EngineCatalogEntry, 0, len(nodes))
	for _, node := range nodes {
		if node.Kind == plugin.EngineCatalogKindBucket {
			buckets = append(buckets, node)
		}
	}
	return buckets, nil
}

func listObjectCatalogLeaves(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.EngineCatalogProvider,
	bucketName, prefix string,
	recursive bool,
) ([]plugin.EngineCatalogEntry, error) {
	nodes, err := catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.ObjectDirectoryPath(resource.ID, bucketName, prefix), plugin.ListOptions{Recursive: recursive})
	if err != nil {
		return nil, err
	}

	objects := make([]plugin.EngineCatalogEntry, 0, len(nodes))
	for _, node := range nodes {
		if node.Role == plugin.EngineCatalogRoleLeaf {
			objects = append(objects, node)
		}
	}
	return objects, nil
}

func resolveObjectCatalogTarget(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.EngineCatalogProvider,
	rawPath string,
) (objectCatalogPathTarget, error) {
	bucketName, objectPath := metapath.SplitObjectPath(rawPath)
	target := objectCatalogPathTarget{Bucket: bucketName}
	if bucketName == "" {
		return target, nil
	}
	if objectPath == "" {
		return target, nil
	}
	node, err := catalogProvider.ResolvePath(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.ObjectItemPath(resource.ID, bucketName, objectPath))
	if err == nil && node != nil && node.Role == plugin.EngineCatalogRoleLeaf {
		target.Object = objectPath
		return target, nil
	}
	target.Prefix = objectPath
	return target, nil
}

func readObjectCatalogLeaf(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.EngineCatalogProvider,
	bucketName, objectPath string,
) ([]plugin.EngineCatalogEntry, error) {
	node, err := catalogProvider.ResolvePath(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.ObjectItemPath(resource.ID, bucketName, objectPath))
	if err != nil {
		return nil, err
	}
	if node == nil || node.Role != plugin.EngineCatalogRoleLeaf {
		return nil, nil
	}
	return []plugin.EngineCatalogEntry{*node}, nil
}

func objectCatalogEntriesToStorageResources(
	objects []plugin.EngineCatalogEntry,
	bucket string,
) []scanresource.StorageResource {
	resources := make([]scanresource.StorageResource, 0, len(objects))
	for _, obj := range objects {
		if scanresource.IgnoreSystemEngineCatalogEntry(obj) {
			continue
		}
		resources = append(resources, scanresource.ObjectStorageResourceFromNode(bucket, obj))
	}
	return resources
}
