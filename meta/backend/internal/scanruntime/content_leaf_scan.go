package scanruntime

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaenrich"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
)

// ScanLeaf consumes resolved catalog facts through the full-scan processor,
// without enumerating a parent or finalizing its range scan state.
func (s *ObjectStorageCatalogRuntime) ScanLeaf(ctx context.Context, resource *commonModels.Engine, tenantID uint, entry plugin.EngineCatalogEntry, scanDepth string, force bool) (scanflow.DispatchResult, error) {
	if entry.Role != plugin.EngineCatalogRoleLeaf || entry.Path.EngineID != resource.ID {
		return scanflow.DispatchResult{}, fmt.Errorf("invalid exact object scan entry")
	}
	ref, err := plugin.RequireObjectLeafPath(entry.Path)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}
	bucket, _, err := plugin.SplitObjectRefPath(ref)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}
	metaenrich.RegisterItemResolvers()
	resources := objectCatalogEntriesToStorageResources([]plugin.EngineCatalogEntry{entry}, bucket)
	if len(resources) == 0 {
		return scanflow.DispatchResult{}, nil
	}
	root, err := metaRepo.EnsureEngineCatalogRootNode(s.repo, tenantID, resource, enginePlugin)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}
	parent, err := s.repo.UpsertNode(tenantID, resource.ID, root, plugin.EngineCatalogTermBucket, bucket, &bucket, scanresource.ObjectBucketNodeAttributes(bucket))
	if err != nil {
		return scanflow.DispatchResult{}, err
	}
	count, extraction, err := s.persistObjectResources(ctx, resource, tenantID, resource.ID, parent, resources, nil, false, scanflow.ScanDepthOrDefault(scanDepth, "deep"), force, scanresource.ParentObjectPath(resources[0].Path), nil, scanflow.EngineCatalogLeafTermForPlugin(enginePlugin, plugin.EngineCatalogTermObject))
	return scanflow.DispatchResult{Items: count, Extraction: extraction}, err
}

func (s *FilesystemCatalogRuntime) ScanLeaf(ctx context.Context, resource *commonModels.Engine, tenantID uint, entry plugin.EngineCatalogEntry, scanDepth string, force bool) (scanflow.DispatchResult, error) {
	if entry.Role != plugin.EngineCatalogRoleLeaf || entry.Path.EngineID != resource.ID {
		return scanflow.DispatchResult{}, fmt.Errorf("invalid exact file scan entry")
	}
	if _, err := plugin.RequireFileLeafPath(entry.Path); err != nil {
		return scanflow.DispatchResult{}, err
	}
	file, ok := scanresource.StorageFileRefFromEntry(entry)
	if !ok {
		return scanflow.DispatchResult{}, nil
	}
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}
	contentReader, ok := enginePlugin.(plugin.ContentReadableProvider)
	if !ok {
		return scanflow.DispatchResult{}, fmt.Errorf("engine %s does not implement ContentReadableProvider", resource.EngineType)
	}
	metaenrich.RegisterItemResolvers()
	_, parent, err := s.ensureFilesystemScanRoot(tenantID, resource, enginePlugin, path.Dir(file.Path))
	if err != nil {
		return scanflow.DispatchResult{}, err
	}
	scanDepth = scanflow.ScanDepthOrDefault(scanDepth, "deep")
	_, persisted, extraction, err := s.scanSingleFileItem(fileSingleItemScanInput{
		ctx: ctx, contentReader: contentReader, connInfo: plugin.ConnectionInfo(resource.ConnectionInfo), resource: resource, tenantID: tenantID,
		parentNode: parent, file: file, itemTerm: scanflow.EngineCatalogLeafTermForPlugin(enginePlugin, plugin.EngineCatalogTermFile), scanDepth: scanDepth, force: force, isDeepScan: strings.EqualFold(scanDepth, "deep"),
	})
	result := scanflow.DispatchResult{Extraction: extraction}
	if persisted {
		result.Items = 1
	}
	return result, err
}
