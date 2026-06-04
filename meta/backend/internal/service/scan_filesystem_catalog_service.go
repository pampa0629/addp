package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scantask"
	"gorm.io/gorm"
)

// FilesystemCatalogScanService 文件系统 catalog 扫描服务。
// 职责：通过 CatalogProvider 扫描文件系统语义存储，并使用 ContentReadableProvider 读取内容识别复合数据项。
type FilesystemCatalogScanService struct {
	db      *gorm.DB
	log     *slog.Logger
	repo    *metaRepo.ScanRepository
	indexer *IndexerService
}

// NewFilesystemCatalogScanService 创建文件系统 catalog 扫描服务。
func NewFilesystemCatalogScanService(
	db *gorm.DB,
	log *slog.Logger,
	repo *metaRepo.ScanRepository,
	indexer *IndexerService,
) *FilesystemCatalogScanService {
	return &FilesystemCatalogScanService{
		db:      db,
		log:     log,
		repo:    repo,
		indexer: indexer,
	}
}

// ScanPaths 扫描文件系统 catalog 路径，识别复合数据项。
func (s *FilesystemCatalogScanService) ScanPaths(
	resource *commonModels.Engine,
	tenantID uint,
	paths []string,
	scanDepth string,
	force bool,
	reporter ScanProgressReporter,
) (int, int, scantask.ExtractionCounts, error) {
	metaenrich.RegisterItemResolvers()

	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		return 0, 0, scantask.ExtractionCounts{}, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}

	catalogProvider, ok := p.(plugin.CatalogProvider)
	if !ok {
		return 0, 0, scantask.ExtractionCounts{}, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
	}
	itemTerm := catalogLeafTermForPlugin(p, plugin.CatalogTermFile)
	contentReader, ok := p.(plugin.ContentReadableProvider)
	if !ok {
		return 0, 0, scantask.ExtractionCounts{}, fmt.Errorf("engine %s does not implement ContentReadableProvider", resource.EngineType)
	}

	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)
	if len(paths) == 0 {
		paths = []string{""}
	}

	resolvedPaths, err := resolveCatalogScanPaths(context.Background(), "未检测到可扫描的路径", paths, nil, nil, reporter)
	if err != nil {
		return 0, 0, scantask.ExtractionCounts{}, err
	}
	paths = resolvedPaths

	totalRoots := 0
	totalItems := 0
	extractionStats := scantask.ExtractionCounts{}

	for i, rootPath := range paths {
		rootPath = metapath.SanitizeFSPath(rootPath)
		if reporter != nil {
			displayPath := rootPath
			if displayPath == "" {
				displayPath = "/"
			}
			reporter.Message(fmt.Sprintf("扫描路径 %s", displayPath))
		}

		if _, _, err := s.listDirectory(context.Background(), resource, catalogProvider, connInfo, rootPath); err != nil {
			s.log.Warn("跳过非目录扫描路径", "path", rootPath, "error", err)
			continue
		}

		_, scanNode, err := s.ensureFilesystemScanRoot(tenantID, resource, p, rootPath)
		if err != nil {
			s.log.Warn("创建根节点失败", "path", rootPath, "error", err)
			continue
		}

		// 标记扫描中
		_ = s.repo.ResetNodeState(scanNode, "running")
		totalRoots++

		// 递归扫描目录
		items, pathExtractionStats, scanErr := s.scanDirectory(context.Background(), contentReader, catalogProvider, connInfo, resource, tenantID, rootPath, scanNode, rootPath == "", itemTerm, scanDepth, force)
		extractionStats = mergeExtractionCounts(extractionStats, pathExtractionStats)
		if scanErr != nil {
			s.log.Warn("扫描目录失败", "path", rootPath, "error", scanErr)
			_ = s.repo.FinalizeNodeState(scanNode, "failed", items, 0, scanErr.Error())
		} else {
			_ = s.repo.FinalizeNodeStateWithDepth(scanNode, "completed", items, 0, "", scanDepth)
		}
		totalItems += items

		if reporter != nil {
			reporter.Advance(rootPath, i+1, len(paths), map[string]interface{}{"items": items})
		}
	}

	return totalRoots, totalItems, extractionStats, nil
}

func (s *FilesystemCatalogScanService) ensureFilesystemScanRoot(tenantID uint, resource *commonModels.Engine, enginePlugin plugin.EnginePlugin, scanPath string) (*models.MetaNode, *models.MetaNode, error) {
	rootNode, err := ensureCatalogRootNodeWithNativeName(s.repo, tenantID, resource, enginePlugin, "/")
	if err != nil {
		return nil, nil, err
	}
	scanPath = metapath.SanitizeFSPath(scanPath)
	if scanPath == "" {
		return rootNode, rootNode, nil
	}
	current := rootNode
	parts := strings.Split(scanPath, "/")
	for i, part := range parts {
		if part == "" || part == "." {
			continue
		}
		fullName := strings.Join(parts[:i+1], "/")
		node, err := s.repo.UpsertNode(
			tenantID,
			resource.ID,
			current,
			"dir",
			part,
			&fullName,
			metacatalog.FileDirectoryNodeAttributes(fullName),
		)
		if err != nil {
			return rootNode, nil, err
		}
		current = node
	}
	return rootNode, current, nil
}

// scanDirectory 递归扫描目录，对每个目录运行 item resolver 链。
// isBucketRoot=true 时跳过独占 whole-scope 识别（bucket 根目录不应被整体识别为一张表）。
func (s *FilesystemCatalogScanService) scanDirectory(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	catalogProvider plugin.CatalogProvider,
	connInfo plugin.ConnectionInfo,
	resource *commonModels.Engine,
	tenantID uint,
	dirPath string,
	parentNode *models.MetaNode,
	isBucketRoot bool,
	itemTerm string,
	scanDepth string,
	force bool,
) (int, scantask.ExtractionCounts, error) {
	files, subdirs, err := s.listDirectory(ctx, resource, catalogProvider, connInfo, dirPath)
	if err != nil {
		return 0, scantask.ExtractionCounts{}, fmt.Errorf("failed to list directory %s: %w", dirPath, err)
	}

	totalItems := 0
	extractionStats := scantask.ExtractionCounts{}

	claimedPaths := metaitem.ResourceClaimSet{}
	scannedFileFullNames := make(map[string]bool, len(files))
	for _, file := range files {
		scannedFileFullNames[metapath.JoinFSPath(parentNode.FullName, file.Name)] = true
	}
	if err := s.repo.HardDeleteChildNodesByFullNames(parentNode.ID, scannedFileFullNames); err != nil {
		s.log.Warn("清理文件路径冲突节点失败", "path", dirPath, "node_id", parentNode.ID, "error", err)
	}
	scannedItemFullNames := make(map[string]bool)
	scannedSubdirFullNames := make(map[string]bool)
	reconcileScannedDirectory := func() {
		if !force {
			return
		}
		if err := s.repo.HardDeleteItemsByNodeExceptFullNames(parentNode.ID, scannedItemFullNames); err != nil {
			s.log.Warn("清理已消失的文件数据项失败", "path", dirPath, "node_id", parentNode.ID, "error", err)
		}
		if err := s.repo.HardDeleteChildNodesExceptFullNames(parentNode.ID, scannedSubdirFullNames); err != nil {
			s.log.Warn("清理已消失的子目录节点失败", "path", dirPath, "node_id", parentNode.ID, "error", err)
		}
	}

	// 根目录允许非独占组合识别（如根目录下的 multi 组件 item），但不允许被目录树 whole-scope 识别整体吞掉。
	// 子目录走完整组合识别入口；只有 whole scope 等明确独占范围时才停止后续探测。
	isDeepScan := strings.EqualFold(scanDepth, "deep")
	if isDeepScan && isBucketRoot {
		detection, err := resolveNonExclusiveScopeItems(ctx, contentReader, connInfo, resource, dirPath, files, subdirs)
		if err != nil {
			s.log.Warn("提取根目录组合数据项信息失败",
				"path", dirPath,
				"error", err,
			)
		}
		if detection != nil {
			for path, claimed := range detection.Claims {
				if claimed {
					claimedPaths[path] = true
				}
			}
			for _, detected := range detection.Items {
				if detected == nil {
					continue
				}
				persisted, fullName, itemExtractionStats := s.persistFileCatalogDetectedItem(ctx, resource, tenantID, parentNode, dirPath, detected, itemTerm, contentReader, connInfo, scanDepth)
				if fullName != "" {
					scannedItemFullNames[fullName] = true
				}
				if persisted {
					totalItems++
				}
				extractionStats = mergeExtractionCounts(extractionStats, itemExtractionStats)
			}
		}
	} else if isDeepScan {
		detection, err := s.resolveFileCatalogDirectoryItems(ctx, contentReader, catalogProvider, connInfo, resource, dirPath, files, subdirs)
		if err != nil {
			s.log.Warn("提取复合数据项信息失败",
				"path", dirPath,
				"error", err,
			)
		}
		if detection != nil {
			for path, claimed := range detection.Claims {
				if claimed {
					claimedPaths[path] = true
				}
			}
			for _, detected := range detection.Items {
				if detected == nil {
					continue
				}
				persisted, fullName, itemExtractionStats := s.persistFileCatalogDetectedItem(ctx, resource, tenantID, parentNode, dirPath, detected, itemTerm, contentReader, connInfo, scanDepth)
				if fullName != "" {
					scannedItemFullNames[fullName] = true
				}
				if persisted {
					totalItems++
				}
				extractionStats = mergeExtractionCounts(extractionStats, itemExtractionStats)
			}
			if detection.Exclusive {
				reconcileScannedDirectory()
				return totalItems, extractionStats, nil
			}
		}
	}

	// 未被组合 item 认领的文件继续逐文件处理。
	for _, file := range files {
		if claimedPaths[file.Path] {
			continue
		}
		detected := metaitem.InferSingleResourceItem(file)
		itemName := file.Name
		fullName := metapath.JoinFSPath(parentNode.FullName, itemName)
		scannedItemFullNames[fullName] = true
		existingItem, itemExists, findErr := s.repo.FindItemByFullName(tenantID, resource.ID, fullName)
		if findErr != nil {
			s.log.Warn("查询文件对象失败", "path", file.Path, "error", findErr)
		}
		if itemExists && !force && !fileItemNeedsScan(existingItem, file, isDeepScan) {
			totalItems++
			continue
		}
		result, err := processDetectedItem(s.repo, s.indexer, s.log).Process(ctx, detectedItemInput{
			Resource:           resource,
			TenantID:           tenantID,
			EngineID:           resource.ID,
			ParentNode:         parentNode,
			ItemType:           itemTerm,
			ItemName:           itemName,
			FullName:           fullName,
			Attributes:         metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(detected))),
			Detected:           detected,
			ContentReader:      contentReader,
			ConnInfo:           connInfo,
			CatalogPath:        file.CatalogPath,
			CatalogPathFor:     func(string) plugin.CatalogPath { return file.CatalogPath },
			PhysicalPath:       file.Path,
			IndexPath:          file.Path,
			IndexRelativePath:  file.Path,
			SizeBytes:          file.Size,
			DataUpdatedAt:      fileModifiedAtPtr(file.ModifiedAt),
			ScanDepth:          scanDepth,
			IncludeAccessIndex: true,
		})
		if err != nil {
			s.log.Warn("保存 single 文件对象失败", "path", file.Path, "error", err)
			continue
		}
		extractionStats = mergeExtractionCounts(extractionStats, result.Extraction)
		totalItems++
		if detected.DataType == datatype.Table && result.Item != nil {
			tableInfo := tableInfoFromMetaAttributes(result.Item.Attributes)
			if tableInfo != nil && len(tableInfo.Fields) > 0 {
				s.log.Info("识别到 single 文件表", "path", file.Path, "name", itemName, "format", detected.Format, "field_count", len(tableInfo.Fields))
			}
		}
	}

	// 递归扫描子目录
	for _, subdir := range subdirs {
		subdirName := subdir.Name
		subdirAttrs := metacatalog.FileDirectoryNodeAttributes(subdir.Path)
		subdirFullName := metapath.JoinFSPath(parentNode.FullName, subdirName)
		scannedSubdirFullNames[subdirFullName] = true
		subdirNode, err := s.repo.UpsertNode(tenantID, resource.ID, parentNode, "dir", subdirName, &subdirFullName, subdirAttrs)
		if err != nil {
			s.log.Warn("创建子目录节点失败", "path", subdir.Path, "error", err)
			continue
		}

		_ = s.repo.ResetNodeState(subdirNode, "running")
		items, subdirExtractionStats, scanErr := s.scanDirectory(ctx, contentReader, catalogProvider, connInfo, resource, tenantID, subdir.Path, subdirNode, false, itemTerm, scanDepth, force)
		extractionStats = mergeExtractionCounts(extractionStats, subdirExtractionStats)
		if scanErr != nil {
			s.log.Warn("递归扫描子目录失败", "path", subdir.Path, "error", scanErr)
			_ = s.repo.FinalizeNodeState(subdirNode, "failed", items, 0, scanErr.Error())
		} else {
			_ = s.repo.FinalizeNodeStateWithDepth(subdirNode, "completed", items, 0, "", scanDepth)
		}
		totalItems += items
	}

	reconcileScannedDirectory()
	return totalItems, extractionStats, nil
}

func fileModifiedAtPtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func fileItemNeedsScan(existing *models.MetaItem, file metaitem.StorageFileRef, isDeepScan bool) bool {
	if existing == nil {
		return true
	}
	if isDeepScan && existing.ScannedDepth != models.ScannedDepthDeep {
		return true
	}
	if existing.SizeBytes != nil && *existing.SizeBytes != file.Size {
		return true
	}
	if !file.ModifiedAt.IsZero() && existing.DataUpdatedAt != nil && file.ModifiedAt.After(*existing.DataUpdatedAt) {
		return true
	}
	if existing.DataUpdatedAt == nil && !file.ModifiedAt.IsZero() {
		return true
	}
	return false
}

func (s *FilesystemCatalogScanService) persistFileCatalogDetectedItem(
	ctx context.Context,
	resource *commonModels.Engine,
	tenantID uint,
	parentNode *models.MetaNode,
	dirPath string,
	detected *metaitem.DetectedItem,
	itemTerm string,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	scanDepth string,
) (bool, string, scantask.ExtractionCounts) {
	itemPlan, ok := metacatalog.PlanFileCatalogDetectedItem(resource.ID, dirPath, detected, itemTerm)
	if !ok {
		return false, "", scantask.ExtractionCounts{}
	}
	result, err := processDetectedItem(s.repo, s.indexer, s.log).Process(ctx, detectedItemInput{
		Resource:           resource,
		TenantID:           tenantID,
		EngineID:           resource.ID,
		ParentNode:         parentNode,
		ItemType:           itemPlan.ItemType,
		ItemName:           itemPlan.ItemName,
		FullName:           itemPlan.FullName,
		Attributes:         itemPlan.Attributes,
		Detected:           detected,
		ContentReader:      contentReader,
		ConnInfo:           connInfo,
		CatalogPathFor:     plugin.FileItemPathForEngine(resource.ID),
		PhysicalPath:       detectedItemContentPath(detected, itemPlan.FullName),
		IndexPath:          itemPlan.FullName,
		IndexRelativePath:  itemPlan.FullName,
		SizeBytes:          itemPlan.SizeBytes,
		ScanDepth:          scanDepth,
		IncludeAccessIndex: true,
	})
	if err != nil {
		s.log.Warn("保存复合数据项失败",
			"path", dirPath,
			"item_type", itemPlan.ItemType,
			"full_name", itemPlan.FullName,
			"error", err,
		)
		return false, itemPlan.FullName, result.Extraction
	}
	s.log.Info("识别到复合数据项",
		"path", dirPath,
		"full_name", itemPlan.FullName,
		"item_type", itemPlan.ItemType,
		"layout", detected.Layout,
		"data_type", detected.DataType,
		"name", itemPlan.ItemName,
	)
	return true, itemPlan.FullName, result.Extraction
}

func (s *FilesystemCatalogScanService) resolveFileCatalogDirectoryItems(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	catalogProvider plugin.CatalogProvider,
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

	return metaitem.ResolveItems(ctx, metaitem.DirectoryResolveInput{
		ContentReader:    contentReader,
		ConnInfo:         connInfo,
		EngineID:         resource.ID,
		CatalogPathFor:   plugin.FileItemPathForEngine(resource.ID),
		DirPath:          dirPath,
		Files:            files,
		Subdirs:          subdirs,
		RecursiveFiles:   recursiveFiles,
		RecursiveSubdirs: recursiveSubdirs,
	})
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
	return metaitem.ResolveNonExclusiveItems(ctx, metaitem.DirectoryResolveInput{
		ContentReader:  contentReader,
		ConnInfo:       connInfo,
		EngineID:       resource.ID,
		CatalogPathFor: plugin.FileItemPathForEngine(resource.ID),
		DirPath:        dirPath,
		Files:          files,
		Subdirs:        subdirs,
	})
}

func (s *FilesystemCatalogScanService) listDirectory(
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

func (s *FilesystemCatalogScanService) listDirectoryRecursive(
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
	files := make([]metaitem.StorageFileRef, 0, len(nodes))
	subdirs := make([]metaitem.StorageDirectoryRef, 0)
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
