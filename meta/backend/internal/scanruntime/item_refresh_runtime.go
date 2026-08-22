package scanruntime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanprocessor"
)

type ItemRefreshRuntime struct {
	repo               *metaRepo.ScanRepository
	indexer            RuntimeIndexer
	log                *slog.Logger
	cadInspector       metaenrich.CADInspector
	containerInspector metaenrich.ContainerInspector
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
	return r.RefreshKnownItemByIDWithPlugin(ctx, nil, resource, tenantID, itemID)
}

func (r *ItemRefreshRuntime) RefreshKnownItemByIDWithPlugin(
	ctx context.Context,
	p plugin.EnginePlugin,
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
	return r.RefreshKnownItemWithPlugin(ctx, p, resource, tenantID, *item, *parentNode)
}

func (r *ItemRefreshRuntime) RefreshKnownItem(
	ctx context.Context,
	resource *commonModels.Engine,
	tenantID uint,
	item models.MetaItem,
	parentNode models.MetaNode,
) (scanprocessor.Result, error) {
	return r.RefreshKnownItemWithPlugin(ctx, nil, resource, tenantID, item, parentNode)
}

func (r *ItemRefreshRuntime) RefreshKnownItemWithPlugin(
	ctx context.Context,
	p plugin.EnginePlugin,
	resource *commonModels.Engine,
	tenantID uint,
	item models.MetaItem,
	parentNode models.MetaNode,
) (scanprocessor.Result, error) {
	if p == nil {
		var err error
		p, err = plugin.Get(resource.EngineType)
		if err != nil {
			return scanprocessor.Result{}, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
		}
	}
	if result, handled, err := r.refreshKnownDynamicSchemaItem(ctx, resource, tenantID, p, item, parentNode); handled || err != nil {
		return result, err
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
	var detected *metaitem.DetectedItem
	var attrs models.JSONMap
	if redetected, ok := r.reDetectKnownWholeScopeItem(ctx, resource, p, contentReader, connInfo, descriptor, physicalPath); ok {
		descriptor = knownItemDescriptorFromDetected(redetected, descriptor)
		physicalPath = scanflow.KnownItemPhysicalPath(descriptor, &item)
		attrs = metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(redetected)))
		restoreKnownItemStorage(attrs, descriptor, &item)
		detected = redetected
	} else {
		descriptor = r.reDetectKnownSingleItemFormat(ctx, contentReader, connInfo, catalogPathFor, descriptor, physicalPath)
		attrs = metaattr.JSONMap(cloneJSONMap(item.Attributes))
		restoreKnownItemStorage(attrs, descriptor, &item)
		detected = scanflow.KnownItemDetectedItem(&item, descriptor)
		metaattr.MergeDataItemAttributes(attrs, metaitem.AttributeInput(detected))
	}
	indexPath := scanflow.KnownItemPhysicalPath(descriptor, &item)
	if descriptor.StorageBucket != "" {
		indexPath = scanflow.KnownItemObjectPath(descriptor, physicalPath)
	}

	return scanprocessor.New(r.repo, r.indexer, r.log).WithCADInspector(r.cadInspector).WithContainerInspector(r.containerInspector).Process(ctx, scanprocessor.KnownItemInput(
		resource,
		tenantID,
		&parentNode,
		item,
		attrs,
		detected,
		contentReader,
		connInfo,
		catalogPathFor,
		physicalPath,
		descriptor.StorageBucket,
		indexPath,
		scanflow.KnownItemSize(descriptor, &item),
	))
}

func (r *ItemRefreshRuntime) refreshKnownDynamicSchemaItem(
	ctx context.Context,
	resource *commonModels.Engine,
	tenantID uint,
	p plugin.EnginePlugin,
	item models.MetaItem,
	parentNode models.MetaNode,
) (scanprocessor.Result, bool, error) {
	samplingProvider, ok := p.(plugin.DynamicSchemaSamplingProvider)
	if !ok || item.ItemType != plugin.CatalogTermCollection {
		return scanprocessor.Result{}, false, nil
	}
	model := scanflow.CatalogModelForPlugin(p)
	if model == nil || len(model.Levels) != 2 {
		return scanprocessor.Result{}, true, fmt.Errorf("dynamic schema item refresh requires a branch-leaf catalog model")
	}
	branchLevel, leafLevel := model.Levels[0], model.Levels[1]
	if branchLevel.Role != plugin.CatalogRoleBranch || leafLevel.Role != plugin.CatalogRoleLeaf ||
		leafLevel.Term != item.ItemType || len(leafLevel.Kinds) != 1 || strings.TrimSpace(leafLevel.Kinds[0]) == "" {
		return scanprocessor.Result{}, true, fmt.Errorf("dynamic schema item refresh target does not match catalog model")
	}
	branchName := strings.TrimSpace(parentNode.Name)
	if branchName == "" || strings.TrimSpace(item.Name) == "" {
		return scanprocessor.Result{}, true, fmt.Errorf("dynamic schema item refresh target is incomplete; rescan the parent node")
	}

	itemPath := plugin.BranchLeafCatalogPath(*model, resource.ID, branchLevel.Term, branchName, leafLevel.Term, leafLevel.Kinds[0], item.Name)
	facts, err := samplingProvider.SampleDynamicSchema(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), itemPath, plugin.CatalogFactsOptions{
		IncludeSamples:    true,
		IncludeStatistics: true,
		IncludeIndexes:    true,
		SampleSize:        100,
	})
	if err != nil {
		return scanprocessor.Result{}, true, fmt.Errorf("dynamic schema sampling failed: %w", err)
	}
	if facts == nil {
		return scanprocessor.Result{}, true, fmt.Errorf("dynamic schema sampling returned no facts")
	}

	attrs := metaattr.BuildDynamicSchemaAttributes(dynamicSchemaAttributesInput(facts))
	var rowCount *int64
	var estimatedRowCount *int64
	var dataUpdatedAt = item.DataUpdatedAt
	var sizeBytes int64
	fieldCount := 0
	if tableInfo := plugin.CatalogFactsTableInfo(facts); tableInfo != nil {
		rowCount = tableInfo.RowCount
		estimatedRowCount = tableInfo.EstimatedRowCount
		dataUpdatedAt = tableInfo.UpdatedAt
		fieldCount = len(tableInfo.Fields)
		if tableInfo.SizeBytes != nil {
			sizeBytes = *tableInfo.SizeBytes
		}
	}
	metaattr.ApplyDynamicSchemaStatistics(attrs, rowCount, estimatedRowCount, sizeBytes)
	metaattr.ApplyBranchLeafItemAttributes(attrs, item.ItemType)

	fullName := strings.TrimSpace(item.FullName)
	if fullName == "" {
		fullName = metapath.ComposeNodeFullName(item.Name, &parentNode, ".")
	}
	refreshed, err := r.repo.UpdateItemByIDWithDepth(
		tenantID,
		item.ID,
		resource.ID,
		&parentNode,
		item.ItemType,
		item.Name,
		fullName,
		attrs,
		rowCount,
		&sizeBytes,
		dataUpdatedAt,
		scanflow.ScanDepthDeep,
	)
	if err != nil {
		return scanprocessor.Result{}, true, err
	}
	return scanprocessor.Result{Item: refreshed, Fields: fieldCount}, true, nil
}

func (r *ItemRefreshRuntime) reDetectKnownWholeScopeItem(
	ctx context.Context,
	resource *commonModels.Engine,
	enginePlugin plugin.EnginePlugin,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	descriptor dataitem.ItemDescriptor,
	physicalPath string,
) (*metaitem.DetectedItem, bool) {
	if resource == nil || descriptor.Layout != format.LayoutWhole || contentReader == nil || strings.TrimSpace(physicalPath) == "" {
		return nil, false
	}
	catalogProvider, ok := enginePlugin.(plugin.CatalogProvider)
	if !ok || scanflow.CatalogLeafTermForPlugin(enginePlugin, "") != plugin.CatalogTermFile {
		return nil, false
	}

	scopePath := strings.Trim(physicalPath, "/")
	files, subdirs, err := listKnownFileDirectory(ctx, resource.ID, catalogProvider, connInfo, scopePath, false)
	if err != nil {
		return nil, false
	}
	var recursiveFiles []metaitem.StorageFileRef
	var recursiveSubdirs []metaitem.StorageDirectoryRef
	if len(subdirs) > 0 {
		recursiveFiles, recursiveSubdirs, err = listKnownFileDirectory(ctx, resource.ID, catalogProvider, connInfo, scopePath, true)
		if err != nil {
			return nil, false
		}
	}

	detection, err := scanflow.DetectFileCatalogDirectoryItems(
		ctx,
		contentReader,
		connInfo,
		resource.ID,
		scopePath,
		files,
		subdirs,
		recursiveFiles,
		recursiveSubdirs,
	)
	if err != nil || detection == nil {
		return nil, false
	}
	for _, item := range detection.Items {
		if item == nil || item.Layout != format.LayoutWhole {
			continue
		}
		if strings.Trim(item.ScopePath, "/") == scopePath || strings.Trim(item.PhysicalPath, "/") == scopePath {
			return item, true
		}
	}
	return nil, false
}

func listKnownFileDirectory(
	ctx context.Context,
	engineID uint,
	catalogProvider plugin.CatalogProvider,
	connInfo plugin.ConnectionInfo,
	dirPath string,
	recursive bool,
) ([]metaitem.StorageFileRef, []metaitem.StorageDirectoryRef, error) {
	nodes, err := catalogProvider.ListChildren(ctx, connInfo, plugin.FileDirectoryPath(engineID, dirPath), plugin.ListOptions{Recursive: recursive})
	if err != nil {
		return nil, nil, err
	}
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
	return files, subdirs, nil
}

func knownItemDescriptorFromDetected(item *metaitem.DetectedItem, previous dataitem.ItemDescriptor) dataitem.ItemDescriptor {
	if item == nil {
		return previous
	}
	return dataitem.ItemDescriptor{
		Layout:             item.Layout,
		DataType:           item.DataType,
		Format:             item.Format,
		PrimaryContentPath: item.PrimaryContentPath,
		ScopePath:          item.ScopePath,
		PhysicalPath:       item.PhysicalPath,
		StorageBucket:      previous.StorageBucket,
		Refs:               item.RefList,
		SizeBytes:          item.SizeBytes,
	}
}

func (r *ItemRefreshRuntime) reDetectKnownSingleItemFormat(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	catalogPathFor func(string) plugin.CatalogPath,
	descriptor dataitem.ItemDescriptor,
	physicalPath string,
) dataitem.ItemDescriptor {
	if descriptor.Layout != format.LayoutSingle || contentReader == nil || catalogPathFor == nil || physicalPath == "" {
		return descriptor
	}
	detectedFormat, err := metaenrich.DetectSingleFileFormat(ctx, contentReader, connInfo, catalogPathFor(physicalPath), physicalPath)
	if err != nil || detectedFormat == format.FormatUnknown {
		return descriptor
	}
	if descriptor.Format == string(detectedFormat) {
		return descriptor
	}
	descriptor.Format = string(detectedFormat)
	descriptor.DataType = dataitem.DefaultDataTypeForFormat(descriptor.Format)
	return descriptor
}

func (r *ItemRefreshRuntime) refreshKnownCatalogFactsItem(
	ctx context.Context,
	resource *commonModels.Engine,
	tenantID uint,
	p plugin.EnginePlugin,
	item models.MetaItem,
	parentNode models.MetaNode,
) (scanprocessor.Result, bool, error) {
	factsProvider, ok := p.(plugin.CatalogFactsProvider)
	if !ok {
		return scanprocessor.Result{}, false, nil
	}

	if scanflow.CatalogLeafTermForPlugin(p, "") != plugin.CatalogTermTable || item.ItemType != plugin.CatalogTermTable {
		return r.refreshKnownDirectCatalogLeafFactsItem(ctx, resource, tenantID, p, factsProvider, item, parentNode)
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
	facts, err := factsProvider.DescribeCatalogFacts(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), path, plugin.CatalogFactsOptions{
		IncludeSpatialFacts: true,
		IncludeStatistics:   true,
		IncludeIndexes:      true,
		IncludeConstraints:  true,
		IncludePartitioning: true,
	})
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
	metaattr.ApplyCatalogFactsCapabilities(attrs, facts)
	if spatialInfo := plugin.CatalogFactsSpatialInfo(facts); spatialInfo != nil {
		metaattr.MergeStandardAttributes(attrs, metaattr.TableDescribeAttributes(metaattr.TableDescribeAttributesInput{
			Spatial: spatialInfo,
		}))
	}

	fullName := metapath.ComposeNodeFullName(tableInfo.Name, &parentNode, ".")
	rowCount := tableInfo.RowCount
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
		rowCount,
		&sizeBytes,
		tableInfo.UpdatedAt,
		scanflow.ScanDepthDeep,
	)
	if err != nil {
		return scanprocessor.Result{}, true, err
	}

	if r.indexer != nil {
		r.indexer.IndexTableAsset(ctx, resource, tenantID, schemaName, tableInfo, tableInfo.Fields, refreshed)
	}

	return scanprocessor.Result{Item: refreshed, Fields: len(tableInfo.Fields)}, true, nil
}

func (r *ItemRefreshRuntime) refreshKnownDirectCatalogLeafFactsItem(
	ctx context.Context,
	resource *commonModels.Engine,
	tenantID uint,
	p plugin.EnginePlugin,
	factsProvider plugin.CatalogFactsProvider,
	item models.MetaItem,
	parentNode models.MetaNode,
) (scanprocessor.Result, bool, error) {
	model := scanflow.CatalogModelForPlugin(p)
	if model == nil || len(model.Levels) != 1 {
		return scanprocessor.Result{}, false, nil
	}
	leaf := model.Levels[0]
	if leaf.Role != plugin.CatalogRoleLeaf || leaf.Term != item.ItemType {
		return scanprocessor.Result{}, false, nil
	}
	if strings.TrimSpace(item.Name) == "" || len(leaf.Kinds) != 1 || strings.TrimSpace(leaf.Kinds[0]) == "" {
		return scanprocessor.Result{}, true, fmt.Errorf("catalog leaf refresh target is incomplete; rescan the parent node")
	}

	path := plugin.CatalogRootPath(*model, resource.ID)
	path.Segments = append(path.Segments, plugin.CatalogSegment{
		Term: leaf.Term,
		Kind: leaf.Kinds[0],
		Name: item.Name,
	})
	facts, err := factsProvider.DescribeCatalogFacts(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), path, plugin.CatalogFactsOptions{
		IncludeStatistics: true,
	})
	if err != nil {
		return scanprocessor.Result{}, true, fmt.Errorf("catalog leaf facts refresh failed: %w", err)
	}
	if facts == nil {
		return scanprocessor.Result{}, true, fmt.Errorf("catalog leaf facts refresh returned no facts")
	}

	fullName := strings.TrimSpace(item.FullName)
	if fullName == "" {
		fullName = item.Name
	}
	dataUpdatedAt := item.DataUpdatedAt
	if facts.UpdatedAt != nil {
		dataUpdatedAt = facts.UpdatedAt
	}
	refreshed, err := r.repo.UpdateItemByIDWithDepth(
		tenantID,
		item.ID,
		resource.ID,
		&parentNode,
		item.ItemType,
		item.Name,
		fullName,
		metaattr.JSONMap(cloneJSONMap(item.Attributes)),
		item.RowCount,
		item.SizeBytes,
		dataUpdatedAt,
		scanflow.ScanDepthDeep,
	)
	if err != nil {
		return scanprocessor.Result{}, true, err
	}
	return scanprocessor.Result{Item: refreshed}, true, nil
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
