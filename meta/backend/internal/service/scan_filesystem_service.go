package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/dataitem"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"gorm.io/gorm"
)

// FileSystemScanService 文件系统扫描服务
// 职责：通过 CatalogProvider 扫描文件系统语义存储，并使用 ContentReadableProvider 读取内容识别复合数据项。
type FileSystemScanService struct {
	db      *gorm.DB
	log     *slog.Logger
	repo    *metaRepo.ScanRepository
	indexer *IndexerService
}

// NewFileSystemScanService 创建文件系统扫描服务
func NewFileSystemScanService(
	db *gorm.DB,
	log *slog.Logger,
	repo *metaRepo.ScanRepository,
	indexer *IndexerService,
) *FileSystemScanService {
	return &FileSystemScanService{
		db:      db,
		log:     log,
		repo:    repo,
		indexer: indexer,
	}
}

// ScanPaths 扫描文件系统路径，识别复合数据项。
func (s *FileSystemScanService) ScanPaths(
	resource *commonModels.Engine,
	tenantID uint,
	paths []string,
	scanDepth string,
	reporter ScanProgressReporter,
) (int, int, error) {
	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		return 0, 0, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}

	catalogProvider, ok := p.(plugin.CatalogProvider)
	if !ok {
		return 0, 0, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
	}
	itemTerm := catalogItemTermForPlugin(p, plugin.CatalogTermFile)
	contentReader, ok := p.(plugin.ContentReadableProvider)
	if !ok {
		return 0, 0, fmt.Errorf("engine %s does not implement ContentReadableProvider", resource.EngineType)
	}

	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)

	// 始终先获取根节点列表，建立 path→name 映射
	allRoots, err := s.listRoots(context.Background(), resource, catalogProvider, connInfo)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list roots: %w", err)
	}
	pathToName := make(map[string]string, len(allRoots))
	for _, r := range allRoots {
		pathToName[r.Path] = r.Name
	}

	// 如果没有指定路径，扫描所有根节点
	if len(paths) == 0 {
		for _, r := range allRoots {
			paths = append(paths, r.Path)
		}
	}

	if len(paths) == 0 {
		if reporter != nil {
			reporter.Message("未检测到可扫描的路径")
			reporter.SetTotal(0)
		}
		return 0, 0, nil
	}

	if reporter != nil {
		reporter.SetTotal(len(paths))
	}

	totalRoots := 0
	totalItems := 0

	for i, rootPath := range paths {
		if reporter != nil {
			reporter.Message(fmt.Sprintf("扫描路径 %s", rootPath))
		}

		// 使用 catalog 根节点返回的名称；插件返回空名时保持空字符串（如 NFS，挂载点透明）
		rootName := pathToName[rootPath]

		// 创建根节点（root）
		// full_name 按规范：NFS 为 ""，本地FS 为 "/" 或 "C:/" 等
		// NFS 引擎的 root 标识为空字符串，路径由引擎配置的 export_path 决定
		rootFullName := metapath.RootFSIdentifier(resource.EngineType, rootPath)
		rootAttrs := models.JSONMap{"path": rootPath}
		rootNode, err := s.repo.UpsertNode(tenantID, resource.ID, nil, "root", rootName, &rootFullName, rootAttrs)
		if err != nil {
			s.log.Warn("创建根节点失败", "path", rootPath, "error", err)
			continue
		}

		// 标记扫描中
		_ = s.repo.ResetNodeState(rootNode, "running")
		totalRoots++

		// 递归扫描目录
		items, scanErr := s.scanDirectory(context.Background(), contentReader, catalogProvider, connInfo, resource, tenantID, rootPath, rootNode, true, itemTerm, strings.EqualFold(scanDepth, "deep"))
		if scanErr != nil {
			s.log.Warn("扫描目录失败", "path", rootPath, "error", scanErr)
			_ = s.repo.FinalizeNodeState(rootNode, "failed", items, 0, scanErr.Error())
		} else {
			_ = s.repo.FinalizeNodeState(rootNode, "completed", items, 0, "")
		}
		totalItems += items

		if reporter != nil {
			reporter.Advance(rootPath, i+1, len(paths), map[string]interface{}{"items": items})
		}
	}

	return totalRoots, totalItems, nil
}

// scanDirectory 递归扫描目录，对每个目录运行 detector 链
// isBucketRoot=true 时跳过 detector 检测（bucket 根目录不应被整体识别为一张表）
func (s *FileSystemScanService) scanDirectory(
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
	includeContentIndex bool,
) (int, error) {
	files, subdirs, err := s.listDirectory(ctx, resource, catalogProvider, connInfo, dirPath)
	if err != nil {
		return 0, fmt.Errorf("failed to list directory %s: %w", dirPath, err)
	}

	totalItems := 0

	claimedPaths := metaitem.ResourceClaimSet{}

	// 根目录允许非独占组合识别（如根目录下的 Shapefile），但不允许被目录树 detector 整体吞掉。
	// 子目录走完整组合识别入口；只有 whole scope 等明确独占范围时才停止后续探测。
	if isBucketRoot {
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
				if s.persistFileSystemDetectedItem(resource, tenantID, parentNode, dirPath, detected, itemTerm) {
					totalItems++
				}
			}
		}
	} else {
		detection, err := s.resolveFileSystemDirectoryItems(ctx, contentReader, catalogProvider, connInfo, resource, dirPath, files, subdirs)
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
				if s.persistFileSystemDetectedItem(resource, tenantID, parentNode, dirPath, detected, itemTerm) {
					totalItems++
				}
			}
			if detection.Exclusive {
				return totalItems, nil
			}
		}
	}

	// 未被组合 detector 认领的文件继续逐文件处理。
	for _, file := range files {
		if claimedPaths[file.Path] {
			continue
		}
		detected := metaitem.InferSingleResourceItem(file)
		if fileAttrs, fields, err := s.enrichSingleFileAttributes(ctx, contentReader, connInfo, resource, file, detected, includeContentIndex); err == nil {
			itemName := file.Name
			fullName := metapath.JoinFSPath(parentNode.FullName, itemName)
			_, upsertErr := s.repo.UpsertItem(
				tenantID, resource.ID, parentNode,
				itemTerm, itemName, fullName,
				fileAttrs, nil, &file.Size, nil,
			)
			if upsertErr != nil {
				s.log.Warn("保存文件对象失败", "path", file.Path, "error", upsertErr)
			} else {
				totalItems++
				if detected.DataType == dataitem.DataTypeTable && len(fields) > 0 {
					s.log.Info("识别到 single 文件表", "path", file.Path, "name", itemName, "format", detected.Format, "field_count", len(fields))
				}
			}
		} else {
			s.log.Warn("提取 single 资源属性失败", "path", file.Path, "error", err)
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
		items, scanErr := s.scanDirectory(ctx, contentReader, catalogProvider, connInfo, resource, tenantID, subdir.Path, subdirNode, false, itemTerm, includeContentIndex)
		if scanErr != nil {
			s.log.Warn("递归扫描子目录失败", "path", subdir.Path, "error", scanErr)
			_ = s.repo.FinalizeNodeState(subdirNode, "failed", items, 0, scanErr.Error())
		} else {
			_ = s.repo.FinalizeNodeState(subdirNode, "completed", items, 0, "")
		}
		totalItems += items
	}

	return totalItems, nil
}

func (s *FileSystemScanService) persistFileSystemDetectedItem(
	resource *commonModels.Engine,
	tenantID uint,
	parentNode *models.MetaNode,
	dirPath string,
	detected *metaitem.DetectedItem,
	itemTerm string,
) bool {
	attrs := metaattr.JSONMap(metaitem.BuildAttributes(detected))
	if len(detected.Fields) > 0 {
		metaattr.SetSchemaFields(attrs, metaattr.FieldAttributesFromFormat(detected.Fields))
	}

	itemName, fullName := metaitem.FileSystemDetectedItemName(dirPath, detected)
	_, upsertErr := s.repo.UpsertItem(
		tenantID, resource.ID, parentNode,
		itemTerm, itemName, fullName,
		attrs, nil, &detected.SizeBytes, nil,
	)
	if upsertErr != nil {
		s.log.Warn("保存复合数据项失败",
			"path", dirPath,
			"item_type", itemTerm,
			"full_name", fullName,
			"error", upsertErr,
		)
		return false
	}
	s.log.Info("识别到复合数据项",
		"path", dirPath,
		"full_name", fullName,
		"item_type", itemTerm,
		"organization", detected.Organization,
		"data_type", detected.DataType,
		"name", itemName,
	)
	return true
}

func (s *FileSystemScanService) enrichSingleFileAttributes(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	resource *commonModels.Engine,
	file plugin.FileEntry,
	detected *metaitem.DetectedItem,
	includeContentIndex bool,
) (models.JSONMap, []format.FieldInfo, error) {
	if detected == nil {
		detected = metaitem.InferSingleResourceItem(file)
	}
	if detected.Organization == dataitem.OrganizationSingle && hasTableProvider(detected.Format) &&
		(detected.DataType == dataitem.DataTypeTable || detected.Format == string(format.FormatJSON)) {
		extract := metaitem.ExtractTableFileSingleFileInfo
		if detected.Format == string(format.FormatJSON) && detected.DataType != dataitem.DataTypeTable {
			extract = metaitem.ExtractTableFileSingleFileInfoStrict
		}
		info, err := extract(ctx, contentReader, connInfo, resource.ID, file.Path, file.Size, includeContentIndex)
		if err != nil {
			s.log.Warn("提取 single 文件表信息失败，使用基础资源属性", "path", file.Path, "format", detected.Format, "error", err)
			return metaattr.JSONMap(metaitem.BuildAttributes(detected)), nil, nil
		}
		if info != nil {
			if info.SizeBytes == nil {
				info.SizeBytes = &file.Size
			}
			sizeBytes := *info.SizeBytes
			detected = &metaitem.DetectedItem{
				Organization:   info.Organization,
				DataType:       info.DataType,
				Format:         info.Format,
				PhysicalPath:   file.Path,
				EntryPath:      info.EntryPath,
				ComponentFiles: info.ComponentFiles,
				SizeBytes:      sizeBytes,
				Fields:         info.Fields,
				Attributes:     info.Attributes,
			}
		}
	}
	attrs := metaattr.JSONMap(metaitem.BuildAttributes(detected))
	if len(detected.Fields) > 0 {
		metaattr.SetSchemaFields(attrs, metaattr.FieldAttributesFromFormat(detected.Fields))
	}
	metaitem.ApplyContainerSummary(attrs, detected)
	if detected.DataType == dataitem.DataTypeContainer && contentReader != nil {
		reader, err := contentReader.OpenContent(ctx, connInfo, metapath.FileCatalogPathFromFSPath(resource.ID, file.Path), plugin.ReadOptions{})
		if err != nil {
			s.log.Warn("枚举容器内部对象失败，保留容器摘要", "path", file.Path, "error", err)
			return attrs, detected.Fields, nil
		}
		defer reader.Close()
		if err := metaitem.EnrichContainerChildren(ctx, attrs, detected, reader); err != nil {
			s.log.Warn("枚举容器内部对象失败，保留容器摘要", "path", file.Path, "error", err)
		}
	}
	return attrs, detected.Fields, nil
}

func hasTableProvider(formatName string) bool {
	if strings.TrimSpace(formatName) == "" {
		return false
	}
	normalized := dataitem.InferFormat("", "", formatName)
	_, err := format.GetTableProvider(format.FormatType(normalized))
	return err == nil
}

func (s *FileSystemScanService) resolveFileSystemDirectoryItems(
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
		ContentReader: contentReader,
		ConnInfo:      connInfo,
		EngineID:      resource.ID,
		DirPath:       dirPath,
		Files:         files,
		Subdirs:       subdirs,
	})
}

func (s *FileSystemScanService) listRoots(
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
		rootPath := node.Path.StringPath()
		if raw := commonJSON.String(node.Attributes, "storage", "path"); raw != "" {
			rootPath = raw
		}
		roots = append(roots, plugin.RootEntry{
			Name: node.Name,
			Path: rootPath,
		})
	}
	return roots, nil
}

func (s *FileSystemScanService) listDirectory(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.CatalogProvider,
	connInfo plugin.ConnectionInfo,
	dirPath string,
) ([]plugin.FileEntry, []plugin.DirEntry, error) {
	nodes, err := catalogProvider.ListChildren(ctx, connInfo, metapath.FileCatalogPathFromFSPath(resource.ID, dirPath), plugin.ListOptions{})
	if err != nil {
		return nil, nil, err
	}
	files := make([]plugin.FileEntry, 0, len(nodes))
	subdirs := make([]plugin.DirEntry, 0, len(nodes))
	for _, node := range nodes {
		nodePath := node.Path.StringPath()
		if raw := commonJSON.String(node.Attributes, "storage", "path"); raw != "" {
			nodePath = raw
		}
		if node.IsContainer {
			subdirs = append(subdirs, plugin.DirEntry{
				Name: node.Name,
				Path: nodePath,
			})
			continue
		}
		if !node.IsItem {
			continue
		}
		size, _ := int64Stat(node.Stats, "size_bytes")
		contentType := commonJSON.String(node.Attributes, "storage", "content_type")
		files = append(files, plugin.FileEntry{
			Name:        node.Name,
			Path:        nodePath,
			Size:        size,
			ContentType: contentType,
		})
	}
	return files, subdirs, nil
}

func (s *FileSystemScanService) listDirectoryRecursive(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.CatalogProvider,
	connInfo plugin.ConnectionInfo,
	dirPath string,
) ([]plugin.FileEntry, []plugin.DirEntry, error) {
	nodes, err := catalogProvider.ListChildren(ctx, connInfo, metapath.FileCatalogPathFromFSPath(resource.ID, dirPath), plugin.ListOptions{Recursive: true})
	if err != nil {
		return nil, nil, err
	}
	files := make([]plugin.FileEntry, 0, len(nodes))
	subdirs := make([]plugin.DirEntry, 0)
	for _, node := range nodes {
		nodePath := node.Path.StringPath()
		if raw := commonJSON.String(node.Attributes, "storage", "path"); raw != "" {
			nodePath = raw
		}
		if node.IsContainer {
			subdirs = append(subdirs, plugin.DirEntry{
				Name: node.Name,
				Path: nodePath,
			})
			continue
		}
		if !node.IsItem {
			continue
		}
		size, _ := int64Stat(node.Stats, "size_bytes")
		contentType := commonJSON.String(node.Attributes, "storage", "content_type")
		files = append(files, plugin.FileEntry{
			Name:        node.Name,
			Path:        nodePath,
			Size:        size,
			ContentType: contentType,
		})
	}
	return files, subdirs, nil
}
