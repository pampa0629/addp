package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
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
	itemTerm := catalogItemTermForPlugin(p, plugin.CatalogTermFile)
	contentReader, ok := p.(plugin.ContentReadableProvider)
	if !ok {
		return 0, 0, scantask.ExtractionCounts{}, fmt.Errorf("engine %s does not implement ContentReadableProvider", resource.EngineType)
	}

	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)
	allRoots, err := s.listRoots(context.Background(), resource, catalogProvider, connInfo)
	if err != nil {
		return 0, 0, scantask.ExtractionCounts{}, fmt.Errorf("failed to list roots: %w", err)
	}
	pathToName := make(map[string]string, len(allRoots))
	for _, r := range allRoots {
		r.Path = metapath.SanitizeFSPath(r.Path)
		pathToName[r.Path] = r.Name
	}

	if len(paths) == 0 {
		for _, r := range allRoots {
			paths = append(paths, r.Path)
		}
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

		// 使用 catalog 根节点返回的展示名；文件系统语义根路径仍保持空字符串。
		rootName := pathToName[rootPath]

		rootNode, scanNode, err := s.ensureFilesystemScanRoot(tenantID, resource.ID, rootName, rootPath)
		if err != nil {
			s.log.Warn("创建根节点失败", "path", rootPath, "error", err)
			continue
		}

		// 标记扫描中
		_ = s.repo.ResetNodeState(rootNode, "running")
		totalRoots++

		// 递归扫描目录
		items, pathExtractionStats, scanErr := s.scanDirectory(context.Background(), contentReader, catalogProvider, connInfo, resource, tenantID, rootPath, scanNode, rootPath == "", itemTerm, scanDepth, force)
		extractionStats = mergeExtractionCounts(extractionStats, pathExtractionStats)
		if scanErr != nil {
			s.log.Warn("扫描目录失败", "path", rootPath, "error", scanErr)
			_ = s.repo.FinalizeNodeState(rootNode, "failed", items, 0, scanErr.Error())
		} else {
			_ = s.repo.FinalizeNodeStateWithDepth(rootNode, "completed", items, 0, "", scanDepth)
		}
		totalItems += items

		if reporter != nil {
			reporter.Advance(rootPath, i+1, len(paths), map[string]interface{}{"items": items})
		}
	}

	return totalRoots, totalItems, extractionStats, nil
}

func (s *FilesystemCatalogScanService) ensureFilesystemScanRoot(tenantID, engineID uint, rootName, scanPath string) (*models.MetaNode, *models.MetaNode, error) {
	rootFullName := ""
	rootName = filesystemRootDisplayName(rootName)
	rootNode, err := s.repo.UpsertNode(tenantID, engineID, nil, "root", rootName, &rootFullName, models.JSONMap{"path": ""})
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
			engineID,
			current,
			"dir",
			part,
			&fullName,
			models.JSONMap{"path": fullName},
		)
		if err != nil {
			return rootNode, nil, err
		}
		current = node
	}
	return rootNode, current, nil
}

func filesystemRootDisplayName(name string) string {
	if metapath.SanitizeFSPath(name) == "" {
		return "/"
	}
	return strings.Trim(name, "/")
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
				if s.persistFileCatalogDetectedItem(resource, tenantID, parentNode, dirPath, detected, itemTerm) {
					totalItems++
				}
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
				if s.persistFileCatalogDetectedItem(resource, tenantID, parentNode, dirPath, detected, itemTerm) {
					totalItems++
				}
			}
			if detection.Exclusive {
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
		existingItem, itemExists, findErr := s.repo.FindItemByFullName(tenantID, resource.ID, fullName)
		if findErr != nil {
			s.log.Warn("查询文件对象失败", "path", file.Path, "error", findErr)
		}
		if itemExists && !force && !fileItemNeedsScan(existingItem, file, isDeepScan) {
			totalItems++
			continue
		}
		result, err := catalogItemProcessor(s.repo, s.indexer, s.log).Process(ctx, catalogSingleItemInput{
			Resource:            resource,
			TenantID:            tenantID,
			EngineID:            resource.ID,
			ParentNode:          parentNode,
			ItemType:            itemTerm,
			ItemName:            itemName,
			FullName:            fullName,
			Attributes:          metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(detected))),
			Detected:            detected,
			ContentReader:       contentReader,
			ConnInfo:            connInfo,
			CatalogPath:         file.CatalogPath,
			CatalogPathFor:      func(string) plugin.CatalogPath { return file.CatalogPath },
			PhysicalPath:        file.Path,
			IndexPath:           file.Path,
			IndexRelativePath:   file.Path,
			SizeBytes:           file.Size,
			DataUpdatedAt:       fileModifiedAtPtr(file.ModifiedAt),
			ScanDepth:           scanDepth,
			IncludeContentIndex: true,
		})
		if err != nil {
			s.log.Warn("保存 single 文件对象失败", "path", file.Path, "error", err)
			continue
		}
		extractionStats = mergeExtractionCounts(extractionStats, result.Extraction)
		totalItems++
		if detected.DataType == dataitem.DataTypeTable && result.Item != nil {
			tableFields := commonJSON.InterfaceSlice(commonJSON.Value(result.Item.Attributes, "type_info.table", "fields"))
			if len(tableFields) > 0 {
				s.log.Info("识别到 single 文件表", "path", file.Path, "name", itemName, "format", detected.Format, "field_count", len(tableFields))
			}
		}
	}

	// 递归扫描子目录
	for _, subdir := range subdirs {
		subdirName := subdir.Name
		subdirAttrs := models.JSONMap{"path": subdir.Path}
		subdirFullName := metapath.JoinFSPath(parentNode.FullName, subdirName)
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

	return totalItems, extractionStats, nil
}

func fileModifiedAtPtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func fileItemNeedsScan(existing *models.MetaItem, file plugin.FileEntry, isDeepScan bool) bool {
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
	resource *commonModels.Engine,
	tenantID uint,
	parentNode *models.MetaNode,
	dirPath string,
	detected *metaitem.DetectedItem,
	itemTerm string,
) bool {
	itemPlan, ok := metacatalog.PlanFileCatalogDetectedItem(resource.ID, dirPath, detected, itemTerm)
	if !ok {
		return false
	}
	sizeVal := itemPlan.SizeBytes
	rowCount := itemRowCountFromAttributes(itemPlan.Attributes)
	_, upsertErr := s.repo.UpsertItemWithDepth(
		tenantID, resource.ID, parentNode,
		itemPlan.ItemType, itemPlan.ItemName, itemPlan.FullName,
		itemPlan.Attributes, rowCount, &sizeVal, nil,
		models.ScannedDepthDeep,
	)
	if upsertErr != nil {
		s.log.Warn("保存复合数据项失败",
			"path", dirPath,
			"item_type", itemPlan.ItemType,
			"full_name", itemPlan.FullName,
			"error", upsertErr,
		)
		return false
	}
	s.log.Info("识别到复合数据项",
		"path", dirPath,
		"full_name", itemPlan.FullName,
		"item_type", itemPlan.ItemType,
		"layout", detected.Layout,
		"data_type", detected.DataType,
		"name", itemPlan.ItemName,
	)
	return true
}

func (s *FilesystemCatalogScanService) resolveFileCatalogDirectoryItems(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	catalogProvider plugin.CatalogProvider,
	connInfo plugin.ConnectionInfo,
	resource *commonModels.Engine,
	dirPath string,
	files []plugin.FileEntry,
	subdirs []plugin.DirEntry,
) (*metaitem.DetectionResult, error) {
	var recursiveFiles []plugin.FileEntry
	var recursiveSubdirs []plugin.DirEntry
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
	files []plugin.FileEntry,
	subdirs []plugin.DirEntry,
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

func (s *FilesystemCatalogScanService) listRoots(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.CatalogProvider,
	connInfo plugin.ConnectionInfo,
) ([]plugin.RootEntry, error) {
	nodes, err := catalogProvider.ListChildren(ctx, connInfo, plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: resource.ID,
	}, plugin.ListOptions{})
	if err != nil {
		return nil, err
	}
	roots := make([]plugin.RootEntry, 0, len(nodes))
	for _, node := range nodes {
		if !node.IsContainer {
			continue
		}
		rootPath := plugin.NormalizeFileCatalogPath(node.Path.StringPath())
		if raw := commonJSON.String(node.Attributes, "storage", "path"); raw != "" {
			rootPath = plugin.NormalizeFileCatalogPath(raw)
		}
		rootName := node.Name
		if plugin.NormalizeFileCatalogPath(rootName) == "" {
			rootName = "/"
		}
		roots = append(roots, plugin.RootEntry{
			Name: rootName,
			Path: rootPath,
		})
	}
	return roots, nil
}

func (s *FilesystemCatalogScanService) listDirectory(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.CatalogProvider,
	connInfo plugin.ConnectionInfo,
	dirPath string,
) ([]plugin.FileEntry, []plugin.DirEntry, error) {
	nodes, err := catalogProvider.ListChildren(ctx, connInfo, plugin.FileDirectoryPath(resource.ID, dirPath), plugin.ListOptions{})
	if err != nil {
		return nil, nil, err
	}
	files := make([]plugin.FileEntry, 0, len(nodes))
	subdirs := make([]plugin.DirEntry, 0, len(nodes))
	for _, node := range nodes {
		if node.IsContainer {
			if dir, ok := plugin.FileCatalogDirectoryFromNode(node); ok {
				subdirs = append(subdirs, dir)
			}
			continue
		}
		if file, ok := plugin.FileCatalogEntryFromNode(node); ok {
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
) ([]plugin.FileEntry, []plugin.DirEntry, error) {
	nodes, err := catalogProvider.ListChildren(ctx, connInfo, plugin.FileDirectoryPath(resource.ID, dirPath), plugin.ListOptions{Recursive: true})
	if err != nil {
		return nil, nil, err
	}
	files := make([]plugin.FileEntry, 0, len(nodes))
	subdirs := make([]plugin.DirEntry, 0)
	for _, node := range nodes {
		if node.IsContainer {
			if dir, ok := plugin.FileCatalogDirectoryFromNode(node); ok {
				subdirs = append(subdirs, dir)
			}
			continue
		}
		if file, ok := plugin.FileCatalogEntryFromNode(node); ok {
			files = append(files, file)
		}
	}
	return files, subdirs, nil
}
