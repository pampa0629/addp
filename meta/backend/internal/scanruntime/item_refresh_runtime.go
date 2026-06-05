package scanruntime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanprocessor"
)

type ItemRefreshRuntime struct {
	repo    *metaRepo.ScanRepository
	indexer scanprocessor.AssetIndexer
	log     *slog.Logger
}

func NewItemRefreshRuntime(repo *metaRepo.ScanRepository, indexer scanprocessor.AssetIndexer, log *slog.Logger) *ItemRefreshRuntime {
	return &ItemRefreshRuntime{
		repo:    repo,
		indexer: indexer,
		log:     log,
	}
}

func (r *ItemRefreshRuntime) RefreshKnownItemByID(
	ctx context.Context,
	resource *commonModels.Engine,
	tenantID uint,
	itemID uint,
) (scanprocessor.Result, error) {
	if itemID == 0 {
		return scanprocessor.Result{}, fmt.Errorf("item_id is required")
	}
	item, err := r.repo.GetItemByID(tenantID, itemID)
	if err != nil {
		return scanprocessor.Result{}, fmt.Errorf("item target not found: %w", err)
	}
	if item.EngineID != resource.ID {
		return scanprocessor.Result{}, fmt.Errorf("item engine_id does not match request engine_id")
	}
	parentNode, err := r.repo.GetNodeByIDForTenant(tenantID, item.NodeID)
	if err != nil {
		return scanprocessor.Result{}, fmt.Errorf("item parent node not found: %w", err)
	}
	return r.RefreshKnownItem(ctx, resource, tenantID, *item, *parentNode)
}

func (r *ItemRefreshRuntime) RefreshKnownItem(
	ctx context.Context,
	resource *commonModels.Engine,
	tenantID uint,
	item models.MetaItem,
	parentNode models.MetaNode,
) (scanprocessor.Result, error) {
	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		return scanprocessor.Result{}, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}
	contentReader, ok := p.(plugin.ContentReadableProvider)
	if !ok {
		return scanprocessor.Result{}, fmt.Errorf("engine %s does not implement ContentReadableProvider", resource.EngineType)
	}

	descriptor := scanflow.KnownItemDescriptorFromAttributes(item.Attributes)
	if err := scanflow.ValidateKnownItemRefreshDescriptor(descriptor, &item); err != nil {
		return scanprocessor.Result{}, err
	}

	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)
	catalogPathFor := scanflow.KnownItemCatalogPathResolver(resource.ID, p, descriptor)
	physicalPath := scanflow.KnownItemPhysicalPath(descriptor, &item)
	indexPath := scanflow.KnownItemPhysicalPath(descriptor, &item)
	if descriptor.StorageBucket != "" {
		indexPath = scanflow.KnownItemObjectPath(descriptor, physicalPath)
	}

	attrs := metaattr.JSONMap(cloneJSONMap(item.Attributes))
	restoreKnownItemStorage(attrs, descriptor, &item)
	return scanprocessor.New(r.repo, r.indexer, r.log).Process(ctx, scanprocessor.Input{
		Resource:           resource,
		TenantID:           tenantID,
		EngineID:           resource.ID,
		ParentNode:         &parentNode,
		ExistingItemID:     item.ID,
		ItemType:           item.ItemType,
		ItemName:           item.Name,
		FullName:           item.FullName,
		Attributes:         attrs,
		Detected:           scanflow.KnownItemDetectedItem(&item, descriptor),
		ContentReader:      contentReader,
		ConnInfo:           connInfo,
		CatalogPathFor:     catalogPathFor,
		PhysicalPath:       physicalPath,
		IndexRootName:      descriptor.StorageBucket,
		IndexPath:          indexPath,
		IndexRelativePath:  strings.Trim(indexPath, "/"),
		SizeBytes:          scanflow.KnownItemSize(descriptor, &item),
		DataUpdatedAt:      item.DataUpdatedAt,
		ScanDepth:          scanflow.ScanDepthDeep,
		IncludeAccessIndex: true,
		StrictDeepEnrich:   true,
	})
}

func restoreKnownItemStorage(attrs models.JSONMap, descriptor dataitem.ItemDescriptor, item *models.MetaItem) {
	if descriptor.StorageBucket != "" {
		metaattr.SetStorage(attrs, "bucket", descriptor.StorageBucket)
	}
	if descriptor.StoragePath != "" {
		metaattr.SetStorage(attrs, "path", descriptor.StoragePath)
	}
	if descriptor.StorageName != "" {
		metaattr.SetStorage(attrs, "name", descriptor.StorageName)
	}
	if descriptor.PhysicalPath != "" {
		metaattr.SetStorage(attrs, "physical_path", descriptor.PhysicalPath)
	}
	if item != nil && item.FullName != "" {
		metaattr.SetItem(attrs, "full_name", item.FullName)
	}
}

func cloneJSONMap(input models.JSONMap) models.JSONMap {
	output := models.JSONMap{}
	for key, value := range input {
		output[key] = value
	}
	return output
}
