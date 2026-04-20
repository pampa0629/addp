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
// 职责：扫描 FileSystemPlugin 类型的存储引擎，识别湖表等复合数据项
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

	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)

	// 始终先获取根节点列表，建立 path→name 映射
	allRoots, err := fsPlugin.ListRoots(context.Background(), connInfo)
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

		// 优先使用 ListRoots 返回的名称，避免从 "/" 推导出空字符串
		rootName := pathToName[rootPath]
		if rootName == "" {
			rootName = strings.Trim(rootPath, "/")
			if idx := strings.LastIndex(rootName, "/"); idx >= 0 {
				rootName = rootName[idx+1:]
			}
		}
		if rootName == "" {
			rootName = rootPath // 最后兜底
		}

		// 创建根节点（bucket）
		rootAttrs := models.JSONMap{"path": rootPath}
		rootNode, err := s.repo.UpsertNode(tenantID, resource.ID, nil, "bucket", rootName, rootName, rootAttrs)
		if err != nil {
			s.log.Warn("创建根节点失败", "path", rootPath, "error", err)
			continue
		}

		// 标记扫描中
		_ = s.repo.ResetNodeState(rootNode, "扫描中")
		totalRoots++

		// 递归扫描目录
		items, scanErr := s.scanDirectory(context.Background(), fsPlugin, connInfo, resource, tenantID, rootPath, rootNode, true)
		if scanErr != nil {
			s.log.Warn("扫描目录失败", "path", rootPath, "error", scanErr)
			_ = s.repo.FinalizeNodeState(rootNode, "扫描失败", items, 0, scanErr.Error())
		} else {
			_ = s.repo.FinalizeNodeState(rootNode, "已扫描", items, 0, "")
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
	connInfo plugin.ConnectionInfo,
	resource *commonModels.Engine,
	tenantID uint,
	dirPath string,
	parentNode *models.MetaNode,
	isBucketRoot bool,
) (int, error) {
	files, subdirs, err := fsPlugin.ListDirectory(ctx, connInfo, dirPath)
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
		if isLakeTableFile(ext) {
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
			fullName := parentNode.FullName + "/" + itemName

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
			// 普通文件 → object
			fileAttrs := models.JSONMap{
				"path":         file.Path,
				"size":         file.Size,
				"content_type": file.ContentType,
			}
			itemName := file.Name
			fullName := parentNode.FullName + "/" + itemName
			_, upsertErr := s.repo.UpsertItem(
				tenantID, resource.ID, parentNode,
				"object", itemName, fullName,
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
		subdirFullName := parentNode.FullName + "/" + subdirName
		subdirNode, err := s.repo.UpsertNode(tenantID, resource.ID, parentNode, "prefix", subdirName, subdirFullName, subdirAttrs)
		if err != nil {
			s.log.Warn("创建子目录节点失败", "path", subdir.Path, "error", err)
			continue
		}

		_ = s.repo.ResetNodeState(subdirNode, "扫描中")
		items, scanErr := s.scanDirectory(ctx, fsPlugin, connInfo, resource, tenantID, subdir.Path, subdirNode, false)
		if scanErr != nil {
			s.log.Warn("递归扫描子目录失败", "path", subdir.Path, "error", scanErr)
			_ = s.repo.FinalizeNodeState(subdirNode, "扫描失败", items, 0, scanErr.Error())
		} else {
			_ = s.repo.FinalizeNodeState(subdirNode, "已扫描", items, 0, "")
		}
		totalItems += items
	}

	return totalItems, nil
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

// isLakeTableFile 判断文件扩展名是否为湖表格式
func isLakeTableFile(ext string) bool {
	switch ext {
	case ".parquet", ".orc", ".avro":
		return true
	}
	return false
}
