package service

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/addp/common/engine/detector"
	"github.com/addp/common/engine/plugin"
	commonParquet "github.com/addp/common/format/parquet"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// FileSystemScanService 文件系统扫描服务
// 职责：通过 CatalogProvider 扫描文件系统语义存储，并使用 FileSystemPlugin 读取内容识别湖表等复合数据项
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

	fsPlugin, ok := p.(plugin.FileSystemPlugin)
	if !ok {
		return 0, 0, fmt.Errorf("engine %s does not implement FileSystemPlugin", resource.EngineType)
	}
	catalogProvider, _ := p.(plugin.CatalogProvider)
	if catalogProvider == nil {
		return 0, 0, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
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

		// 使用 ListRoots 返回的名称；插件返回空名时保持空字符串（如 NFS，挂载点透明）
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
		items, scanErr := s.scanDirectory(context.Background(), fsPlugin, catalogProvider, connInfo, resource, tenantID, rootPath, rootNode, true)
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
	fsPlugin plugin.FileSystemPlugin,
	catalogProvider plugin.CatalogProvider,
	connInfo plugin.ConnectionInfo,
	resource *commonModels.Engine,
	tenantID uint,
	dirPath string,
	parentNode *models.MetaNode,
	isBucketRoot bool,
) (int, error) {
	files, subdirs, err := s.listDirectory(ctx, resource, fsPlugin, catalogProvider, connInfo, dirPath)
	if err != nil {
		return 0, fmt.Errorf("failed to list directory %s: %w", dirPath, err)
	}

	totalItems := 0

	// bucket 根目录不参与 detector 检测（避免把整个 bucket 识别为一张表）
	// 子目录才走 detector 链（模式 A：目录即表）
	if !isBucketRoot {
		detectors := detector.GetAll()
		for _, d := range detectors {
			if !d.Detect(ctx, files, subdirs) {
				continue
			}
			// 提取元信息
			info, err := d.ExtractItemInfo(ctx, fsPlugin, connInfo, dirPath, files)
			if err != nil {
				s.log.Warn("提取复合数据项信息失败",
					"path", dirPath,
					"item_type", d.ItemType(),
					"error", err,
				)
				break
			}

			// 计算总大小
			var totalSize int64
			for _, f := range files {
				totalSize += f.Size
			}

			// 构建 attributes
			attrs := models.JSONMap{}
			for k, v := range info.Attributes {
				attrs[k] = v
			}
			if _, ok := attrs["physical_path"]; !ok {
				attrs["physical_path"] = dirPath
			}

			// 存储 fields 到 attributes
			if len(info.Fields) > 0 {
				fieldsData := make([]map[string]interface{}, 0, len(info.Fields))
				for _, f := range info.Fields {
					fieldsData = append(fieldsData, map[string]interface{}{
						"name":          f.Name,
						"type":          string(f.Type),
						"original_type": f.OriginalType,
						"nullable":      f.Nullable,
					})
				}
				attrs["fields"] = fieldsData
			}

			itemName, fullName := inferItemName(dirPath)

			_, upsertErr := s.repo.UpsertItem(
				tenantID, resource.ID, parentNode,
				d.ItemType(), itemName, fullName,
				attrs, nil, &totalSize, nil,
			)
			if upsertErr != nil {
				s.log.Warn("保存复合数据项失败",
					"path", dirPath,
					"item_type", d.ItemType(),
					"error", upsertErr,
				)
			} else {
				totalItems++
				s.log.Info("识别到复合数据项",
					"path", dirPath,
					"item_type", d.ItemType(),
					"name", itemName,
				)
			}
			// 匹配到 detector 后不再递归（整个目录作为一个数据项）
			return totalItems, nil
		}
	}

	// 未匹配到任何 detector，逐文件处理（模式 B + 普通文件）
	for _, file := range files {
		ext := strings.ToLower(filepath.Ext(file.Name))
		if commonParquet.IsLakeTableExt(ext) {
			// 模式 B：单个结构化文件识别为 lake_table
			info, err := commonParquet.ExtractSingleFileInfo(ctx, fsPlugin, connInfo, file.Path, file.Size)
			if err != nil {
				s.log.Warn("提取单文件湖表信息失败", "path", file.Path, "error", err)
			}

			attrs := models.JSONMap{}
			if info != nil {
				for k, v := range info.Attributes {
					attrs[k] = v
				}
				if len(info.Fields) > 0 {
					fieldsData := make([]map[string]interface{}, 0, len(info.Fields))
					for _, f := range info.Fields {
						fieldsData = append(fieldsData, map[string]interface{}{
							"name":          f.Name,
							"type":          string(f.Type),
							"original_type": f.OriginalType,
							"nullable":      f.Nullable,
						})
					}
					attrs["fields"] = fieldsData
				}
			} else {
				attrs["format"] = ext[1:] // 去掉点号
				attrs["mode"] = "file"
				attrs["file_count"] = 1
				attrs["total_size"] = file.Size
				attrs["physical_path"] = file.Path
			}

			// 文件名去掉扩展名作为逻辑表名
			itemName := strings.TrimSuffix(file.Name, filepath.Ext(file.Name))
			fullName := joinFSPath(parentNode.FullName, itemName)

			_, upsertErr := s.repo.UpsertItem(
				tenantID, resource.ID, parentNode,
				"lake_table", itemName, fullName,
				attrs, nil, &file.Size, nil,
			)
			if upsertErr != nil {
				s.log.Warn("保存单文件湖表失败", "path", file.Path, "error", upsertErr)
			} else {
				totalItems++
				s.log.Info("识别到单文件湖表", "path", file.Path, "name", itemName)
			}
		} else {
			// 普通文件 → file
			fileAttrs := models.JSONMap{
				"path":         file.Path,
				"size":         file.Size,
				"content_type": file.ContentType,
			}
			itemName := file.Name
			fullName := joinFSPath(parentNode.FullName, itemName)
			_, upsertErr := s.repo.UpsertItem(
				tenantID, resource.ID, parentNode,
				"file", itemName, fullName,
				fileAttrs, nil, &file.Size, nil,
			)
			if upsertErr != nil {
				s.log.Warn("保存文件对象失败", "path", file.Path, "error", upsertErr)
			} else {
				totalItems++
			}
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
		items, scanErr := s.scanDirectory(ctx, fsPlugin, catalogProvider, connInfo, resource, tenantID, subdir.Path, subdirNode, false)
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
		if raw, ok := node.Attributes["path"].(string); ok && raw != "" {
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
	fsPlugin plugin.FileSystemPlugin,
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
		if raw, ok := node.Attributes["path"].(string); ok && raw != "" {
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
		contentType, _ := node.Attributes["content_type"].(string)
		files = append(files, plugin.FileEntry{
			Name:        node.Name,
			Path:        nodePath,
			Size:        size,
			ContentType: contentType,
		})
	}
	return files, subdirs, nil
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
