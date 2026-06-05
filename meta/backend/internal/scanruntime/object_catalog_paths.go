package scanruntime

import (
	"context"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metapath"
)

type objectCatalogPathTarget struct {
	Bucket string
	Prefix string
	Object string
}

func listObjectCatalogBucketNodes(ctx context.Context, resource *commonModels.Engine, catalogProvider plugin.CatalogProvider) ([]plugin.CatalogEntry, error) {
	nodes, err := catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.ObjectRootPath(resource.ID), plugin.ListOptions{})
	if err != nil {
		return nil, err
	}

	buckets := make([]plugin.CatalogEntry, 0, len(nodes))
	for _, node := range nodes {
		if node.Kind == plugin.CatalogKindBucket {
			buckets = append(buckets, node)
		}
	}
	return buckets, nil
}

func listObjectCatalogLeaves(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.CatalogProvider,
	bucketName, prefix string,
	recursive bool,
) ([]plugin.CatalogEntry, error) {
	nodes, err := catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.ObjectDirectoryPath(resource.ID, bucketName, prefix), plugin.ListOptions{Recursive: recursive})
	if err != nil {
		return nil, err
	}

	objects := make([]plugin.CatalogEntry, 0, len(nodes))
	for _, node := range nodes {
		if node.Role == plugin.CatalogRoleLeaf {
			objects = append(objects, node)
		}
	}
	return objects, nil
}

func resolveObjectCatalogTarget(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.CatalogProvider,
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
	if err == nil && node != nil && node.Role == plugin.CatalogRoleLeaf {
		target.Object = objectPath
		return target, nil
	}
	target.Prefix = objectPath
	return target, nil
}

func readObjectCatalogLeaf(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.CatalogProvider,
	bucketName, objectPath string,
) ([]plugin.CatalogEntry, error) {
	node, err := catalogProvider.ResolvePath(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.ObjectItemPath(resource.ID, bucketName, objectPath))
	if err != nil {
		return nil, err
	}
	if node == nil || node.Role != plugin.CatalogRoleLeaf {
		return nil, nil
	}
	return []plugin.CatalogEntry{*node}, nil
}

func objectCatalogEntriesToStorageResources(
	objects []plugin.CatalogEntry,
	bucket string,
) []metacatalog.StorageResource {
	resources := make([]metacatalog.StorageResource, 0, len(objects))
	for _, obj := range objects {
		resources = append(resources, metacatalog.ObjectStorageResourceFromNode(bucket, obj))
	}
	return resources
}
