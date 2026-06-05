package scanruntime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanprocessor"
)

type ItemRefreshRuntime struct {
	repo    *metaRepo.ScanRepository
	indexer RuntimeIndexer
	log     *slog.Logger
}

func NewItemRefreshRuntime(repo *metaRepo.ScanRepository, indexer RuntimeIndexer, log *slog.Logger) *ItemRefreshRuntime {
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
	if result, handled, err := r.refreshKnownCatalogFactsItem(ctx, resource, tenantID, p, item, parentNode); handled || err != nil {
		return result, err
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
	return scanprocessor.New(r.repo, r.indexer, r.log).Process(ctx, scanprocessor.KnownItemInput(
		resource,
		tenantID,
		&parentNode,
		item,
		attrs,
		scanflow.KnownItemDetectedItem(&item, descriptor),
		contentReader,
		connInfo,
		catalogPathFor,
		physicalPath,
		descriptor.StorageBucket,
		indexPath,
		scanflow.KnownItemSize(descriptor, &item),
	))
}

func (r *ItemRefreshRuntime) refreshKnownCatalogFactsItem(
	ctx context.Context,
	resource *commonModels.Engine,
	tenantID uint,
	p plugin.EnginePlugin,
	item models.MetaItem,
	parentNode models.MetaNode,
) (scanprocessor.Result, bool, error) {
	if scanflow.CatalogLeafTermForPlugin(p, "") != plugin.CatalogTermTable || item.ItemType != plugin.CatalogTermTable {
		return scanprocessor.Result{}, false, nil
	}
	factsProvider, ok := p.(plugin.CatalogFactsProvider)
	if !ok {
		return scanprocessor.Result{}, true, fmt.Errorf("engine %s does not implement CatalogFactsProvider", resource.EngineType)
	}

	schemaName := parentNode.FullName
	if schemaName == "" {
		schemaName = parentNode.Name
	}
	if schemaName == "" || item.Name == "" {
		return scanprocessor.Result{}, true, fmt.Errorf("table refresh target is incomplete; rescan the parent node")
	}

	namespaceTerm := scanflow.NamespaceTermForPlugin(p)
	path := plugin.TabularItemPath(resource.ID, namespaceTerm, schemaName, item.Name)
	facts, err := factsProvider.DescribeCatalogFacts(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), path, plugin.CatalogFactsOptions{IncludeSpatialFacts: true})
	if err != nil {
		return scanprocessor.Result{}, true, fmt.Errorf("字段扫描失败: %w", err)
	}

	tableInfo := datatype.TableInfo{}
	if existingTableInfo := tableInfoFromMetaAttributes(item.Attributes); existingTableInfo != nil {
		tableInfo = *existingTableInfo
	}
	tableInfo.Name = item.Name
	if factsTable := plugin.CatalogFactsTableInfo(facts); factsTable != nil {
		tableInfo = mergeDatabaseTableInfo(tableInfo, *factsTable)
	}
	tableInfo.Kind = normalizedTableKind(tableInfo)
	primaryKeyColumns := []string{}
	for _, field := range tableInfo.Fields {
		if field.PrimaryKey {
			primaryKeyColumns = append(primaryKeyColumns, field.Name)
		}
	}
	tableInfo.PrimaryKey = primaryKeyColumns

	attrs := tableItemAttributes(schemaName, tableInfo)
	if item.Attributes != nil {
		attrs = metaattr.JSONMap(cloneJSONMap(item.Attributes))
	}
	metaattr.SetStorage(attrs, "schema_name", schemaName)
	metaattr.ApplyTableItemAttributes(attrs, &tableInfo)
	if spatialInfo := plugin.CatalogFactsSpatialInfo(facts); spatialInfo != nil {
		metaattr.MergeStandardAttributes(attrs, metaattr.TableDescribeAttributes(metaattr.TableDescribeAttributesInput{
			Spatial: spatialInfo,
		}))
	}

	fullName := metapath.ComposeNodeFullName(tableInfo.Name, &parentNode, ".")
	rowCount := derefInt64Ptr(tableInfo.RowCount)
	sizeBytes := derefInt64Ptr(tableInfo.SizeBytes)
	refreshed, err := r.repo.UpdateItemByIDWithDepth(
		tenantID,
		item.ID,
		resource.ID,
		&parentNode,
		item.ItemType,
		tableInfo.Name,
		fullName,
		attrs,
		&rowCount,
		&sizeBytes,
		tableInfo.UpdatedAt,
		scanflow.ScanDepthDeep,
	)
	if err != nil {
		return scanprocessor.Result{}, true, err
	}

	if r.indexer != nil {
		r.indexer.IndexTableAsset(resource, tenantID, schemaName, tableInfo, tableInfo.Fields, refreshed)
	}

	return scanprocessor.Result{Item: refreshed, Fields: len(tableInfo.Fields)}, true, nil
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
