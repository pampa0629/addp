package service

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	commonAttrs "github.com/addp/common/attributes"
	"github.com/addp/common/dataitem"
	_ "github.com/addp/common/dataitem/shapefile"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonParquet "github.com/addp/common/format/parquet"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// FileSystemScanService 文件系统扫描服务
// 职责：通过 CatalogProvider 扫描文件系统语义存储，并使用 ContentReadableProvider 读取内容识别湖表等复合数据项
type FileSystemScanService struct {
	db      *gorm.DB
	log     *slog.Logger
	repo    *ScanRepository
	indexer *IndexerService
}

// NewFileSystemScanService 创建文件系统扫描服务
func NewFileSystemScanService(
	db *gorm.DB,
	log *slog.Logger,
	repo *ScanRepository,
	indexer *IndexerService,
) *FileSystemScanService {
	return &FileSystemScanService{
		db:      db,
		log:     log,
		repo:    repo,
		indexer: indexer,
	}
}

// ScanPaths 扫描文件系统路径，识别湖表等复合数据项
func (s *FileSystemScanService) ScanPaths(
	resource *commonModels.Engine,
	tenantID uint,
	paths []string,
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
		rootFullName := rootFSIdentifier(resource.EngineType, rootPath)
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
		items, scanErr := s.scanDirectory(context.Background(), contentReader, catalogProvider, connInfo, resource, tenantID, rootPath, rootNode, true)
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
) (int, error) {
	files, subdirs, err := s.listDirectory(ctx, resource, catalogProvider, connInfo, dirPath)
	if err != nil {
		return 0, fmt.Errorf("failed to list directory %s: %w", dirPath, err)
	}

	totalItems := 0

	claimedPaths := dataitem.ResourceClaimSet{}

	// 根目录允许非独占组合识别（如根目录下的 Shapefile），但不允许被目录树 detector 整体吞掉。
	// 子目录走完整组合识别入口；只有 directory_tree 等明确独占范围时才停止后续探测。
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
				if s.persistFileSystemDetectedItem(resource, tenantID, parentNode, dirPath, detected) {
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
				if s.persistFileSystemDetectedItem(resource, tenantID, parentNode, dirPath, detected) {
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
		detected := dataitem.InferSingleFileItem(file)
		if fileAttrs, fields, err := s.enrichSingleFileAttributes(ctx, contentReader, connInfo, resource, file, detected); err == nil {
			itemType := fileSystemSingleFileItemType(detected)
			itemName := file.Name
			fullName := joinFSPath(parentNode.FullName, itemName)
			_, upsertErr := s.repo.UpsertItem(
				tenantID, resource.ID, parentNode,
				itemType, itemName, fullName,
				fileAttrs, nil, &file.Size, nil,
			)
			if upsertErr != nil {
				s.log.Warn("保存文件对象失败", "path", file.Path, "error", upsertErr)
			} else {
				totalItems++
				if itemType == "lake_table" && len(fields) > 0 {
					s.log.Info("识别到单文件湖表", "path", file.Path, "name", itemName, "field_count", len(fields))
				}
			}
		} else {
			s.log.Warn("提取单文件属性失败", "path", file.Path, "error", err)
		}
	}

	// 递归扫描子目录
	for _, subdir := range subdirs {
		subdirName := subdir.Name
		subdirAttrs := models.JSONMap{"path": subdir.Path}
		subdirFullName := joinFSPath(parentNode.FullName, subdirName)
		subdirNode, err := s.repo.UpsertNode(tenantID, resource.ID, parentNode, "dir", subdirName, &subdirFullName, subdirAttrs)
		if err != nil {
			s.log.Warn("创建子目录节点失败", "path", subdir.Path, "error", err)
			continue
		}

		_ = s.repo.ResetNodeState(subdirNode, "running")
		items, scanErr := s.scanDirectory(ctx, contentReader, catalogProvider, connInfo, resource, tenantID, subdir.Path, subdirNode, false)
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
	detected *dataitem.DetectedItem,
) bool {
	attrs := toJSONMap(dataitem.BuildAttributes(detected))
	if len(detected.Fields) > 0 {
		setSchemaFields(attrs, fieldAttributesFromFormat(detected.Fields))
	}

	itemName, fullName := inferDetectedItemName(dirPath, detected)
	_, upsertErr := s.repo.UpsertItem(
		tenantID, resource.ID, parentNode,
		detected.ItemType, itemName, fullName,
		attrs, nil, &detected.SizeBytes, nil,
	)
	if upsertErr != nil {
		s.log.Warn("保存复合数据项失败",
			"path", dirPath,
			"item_type", detected.ItemType,
			"entry_path", detected.EntryPath,
			"error", upsertErr,
		)
		return false
	}
	s.log.Info("识别到复合数据项",
		"path", dirPath,
		"entry_path", detected.EntryPath,
		"item_type", detected.ItemType,
		"composition_type", detected.CompositionType,
		"data_family", detected.DataFamily,
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
	detected *dataitem.DetectedItem,
) (models.JSONMap, []format.FieldInfo, error) {
	if detected == nil {
		detected = dataitem.InferSingleFileItem(file)
	}
	if detected.ItemType == "lake_table" && commonParquet.IsLakeTableFileType(detected.Format) {
		info, err := commonParquet.ExtractSingleFileInfo(ctx, contentReader, connInfo, resource.ID, file.Path, file.Size)
		if err != nil {
			s.log.Warn("提取单文件湖表信息失败，使用基础文件属性", "path", file.Path, "error", err)
			return toJSONMap(dataitem.BuildAttributes(detected)), nil, nil
		}
		if info != nil {
			if info.SizeBytes == nil {
				info.SizeBytes = &file.Size
			}
			sizeBytes := *info.SizeBytes
			detected = &dataitem.DetectedItem{
				ItemType:        "lake_table",
				CompositionType: info.CompositionType,
				DataFamily:      info.DataFamily,
				Format:          info.Format,
				PhysicalPath:    file.Path,
				EntryPath:       info.EntryPath,
				ComponentFiles:  info.ComponentFiles,
				SizeBytes:       sizeBytes,
				Fields:          info.Fields,
				Attributes:      info.Attributes,
			}
		}
	}
	attrs := toJSONMap(dataitem.BuildAttributes(detected))
	if len(detected.Fields) > 0 {
		setSchemaFields(attrs, fieldAttributesFromFormat(detected.Fields))
	}
	return attrs, detected.Fields, nil
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
) (*dataitem.DetectionResult, error) {
	var recursiveFiles []plugin.FileEntry
	var recursiveSubdirs []plugin.DirEntry
	if len(subdirs) > 0 {
		var err error
		recursiveFiles, recursiveSubdirs, err = s.listDirectoryRecursive(ctx, resource, catalogProvider, connInfo, dirPath)
		if err != nil {
			return nil, err
		}
	}

	return dataitem.ResolveItems(ctx, dataitem.DirectoryResolveInput{
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
) (*dataitem.DetectionResult, error) {
	result := &dataitem.DetectionResult{
		Items:  []*dataitem.DetectedItem{},
		Claims: dataitem.ResourceClaimSet{},
	}
	input := dataitem.DirectoryResolveInput{
		ContentReader: contentReader,
		ConnInfo:      connInfo,
		EngineID:      resource.ID,
		DirPath:       dirPath,
		Files:         files,
		Subdirs:       subdirs,
	}
	for _, detector := range dataitem.GetAll() {
		scoped, ok := detector.(dataitem.ScopeItemDetector)
		if !ok {
			continue
		}
		scopeResult, err := scoped.ResolveItems(ctx, input)
		if err != nil {
			return nil, err
		}
		if scopeResult == nil || scopeResult.Exclusive {
			continue
		}
		for _, item := range scopeResult.Items {
			if item == nil || item.CompositionType == dataitem.CompositionTypeDirectoryTree {
				continue
			}
			result.Items = append(result.Items, item)
		}
		for path, claimed := range scopeResult.Claims {
			if claimed {
				result.Claims[path] = true
			}
		}
	}
	return result, nil
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
		if raw := commonAttrs.String(node.Attributes, "storage", "path"); raw != "" {
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
	nodes, err := catalogProvider.ListChildren(ctx, connInfo, fileCatalogPathFromFSPath(resource.ID, dirPath), plugin.ListOptions{})
	if err != nil {
		return nil, nil, err
	}
	files := make([]plugin.FileEntry, 0, len(nodes))
	subdirs := make([]plugin.DirEntry, 0, len(nodes))
	for _, node := range nodes {
		nodePath := node.Path.StringPath()
		if raw := commonAttrs.String(node.Attributes, "storage", "path"); raw != "" {
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
		contentType := commonAttrs.String(node.Attributes, "storage", "content_type")
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
	nodes, err := catalogProvider.ListChildren(ctx, connInfo, fileCatalogPathFromFSPath(resource.ID, dirPath), plugin.ListOptions{Recursive: true})
	if err != nil {
		return nil, nil, err
	}
	files := make([]plugin.FileEntry, 0, len(nodes))
	subdirs := make([]plugin.DirEntry, 0)
	for _, node := range nodes {
		nodePath := node.Path.StringPath()
		if raw := commonAttrs.String(node.Attributes, "storage", "path"); raw != "" {
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
		contentType := commonAttrs.String(node.Attributes, "storage", "content_type")
		files = append(files, plugin.FileEntry{
			Name:        node.Name,
			Path:        nodePath,
			Size:        size,
			ContentType: contentType,
		})
	}
	return files, subdirs, nil
}

func toJSONMap(attrs map[string]interface{}) models.JSONMap {
	result := models.JSONMap{}
	for k, v := range attrs {
		result[k] = v
	}
	return result
}

func fieldAttributesFromFormat(fields []format.FieldInfo) []map[string]interface{} {
	fieldsData := make([]map[string]interface{}, 0, len(fields))
	for _, f := range fields {
		fieldsData = append(fieldsData, map[string]interface{}{
			"name":          f.Name,
			"type":          string(f.Type),
			"original_type": f.OriginalType,
			"nullable":      f.Nullable,
		})
	}
	return fieldsData
}

func setSchemaFields(attrs models.JSONMap, fields []map[string]interface{}) {
	if attrs == nil || len(fields) == 0 {
		return
	}
	upsertSection(attrs, "schema", map[string]interface{}{"fields": fields})
}

func fileCatalogPathFromFSPath(engineID uint, rawPath string) plugin.CatalogPath {
	path := plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: engineID,
		Segments: []plugin.CatalogSegment{{
			Term: plugin.CatalogTermRoot,
			Kind: plugin.CatalogKindRoot,
			Name: "/",
		}},
	}
	trimmed := strings.Trim(rawPath, "/")
	if trimmed == "" || trimmed == "." {
		return path
	}
	for _, part := range strings.Split(trimmed, "/") {
		if part == "" {
			continue
		}
		path.Segments = append(path.Segments, plugin.CatalogSegment{
			Term: plugin.CatalogTermPath,
			Kind: plugin.CatalogKindPrefix,
			Name: part,
		})
	}
	return path
}

// joinFSPath 拼接文件系统路径
// 规范：full_name = root + path + name，root 为 "" 时不加前缀 "/"
func joinFSPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}

// rootFSIdentifier 返回文件系统根节点的 full_name 标识
// NFS: "" (挂载点由引擎配置决定，不进入路径)
// 本地FS: "/" (Linux/macOS) 或 "C:/" 等 (Windows，直接用 rootPath)
func rootFSIdentifier(engineType, rootPath string) string {
	switch strings.ToLower(engineType) {
	case "nfs", "nas":
		return ""
	default:
		// 本地文件系统：rootPath 本身就是 root 标识（如 "/" 或 "C:/"）
		return rootPath
	}
}

// inferItemName 从路径推断数据项名称
// 路径格式：bucket/schema/table/ → name="table", fullName="bucket/schema/table"
func inferItemName(dirPath string) (name, fullName string) {
	cleaned := strings.Trim(dirPath, "/")
	parts := strings.Split(cleaned, "/")
	if len(parts) == 0 {
		return "unknown", dirPath
	}
	name = parts[len(parts)-1]
	fullName = cleaned
	return
}

func inferDetectedItemName(dirPath string, item *dataitem.DetectedItem) (name, fullName string) {
	if item == nil {
		return inferItemName(dirPath)
	}
	switch item.CompositionType {
	case dataitem.CompositionTypeSingleFile, dataitem.CompositionTypeMultiFile, dataitem.CompositionTypeContainerFile:
		if item.EntryPath != "" {
			cleaned := strings.Trim(item.EntryPath, "/")
			if cleaned != "" {
				return filepath.Base(cleaned), cleaned
			}
		}
	case dataitem.CompositionTypeDirectoryTree:
		return inferItemName(dirPath)
	}
	if item.EntryPath != "" {
		cleaned := strings.Trim(item.EntryPath, "/")
		if cleaned != "" {
			return filepath.Base(cleaned), cleaned
		}
	}
	return inferItemName(dirPath)
}

func fileSystemSingleFileItemType(item *dataitem.DetectedItem) string {
	if item == nil {
		return "file"
	}
	if item.ItemType != "" {
		return item.ItemType
	}
	if rule, ok := dataitem.MatchBuiltinSingleFileRule(item.Format); ok &&
		rule.CompositionType == dataitem.CompositionTypeSingleFile &&
		rule.ItemType != "" {
		return rule.ItemType
	}
	return "file"
}
