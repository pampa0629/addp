package scanruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanprocessor"
)

// ScanDirectory 递归扫描目录，对每个目录运行 item resolver 链。
func (s *FilesystemCatalogRuntime) ScanDirectory(
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
) (int, scanflow.ExtractionCounts, error) {
	files, subdirs, err := s.ListDirectory(ctx, resource, catalogProvider, connInfo, dirPath)
	if err != nil {
		return 0, scanflow.ExtractionCounts{}, fmt.Errorf("failed to list directory %s: %w", dirPath, err)
	}

	totalItems := 0
	extractionStats := scanflow.ExtractionCounts{}

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
				persisted, fullName, itemExtractionStats := s.PersistFileCatalogDetectedItem(ctx, resource, tenantID, parentNode, dirPath, detected, itemTerm, contentReader, connInfo, scanDepth)
				if fullName != "" {
					scannedItemFullNames[fullName] = true
				}
				if persisted {
					totalItems++
				}
				extractionStats = scanflow.MergeExtractionCounts(extractionStats, itemExtractionStats)
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
				persisted, fullName, itemExtractionStats := s.PersistFileCatalogDetectedItem(ctx, resource, tenantID, parentNode, dirPath, detected, itemTerm, contentReader, connInfo, scanDepth)
				if fullName != "" {
					scannedItemFullNames[fullName] = true
				}
				if persisted {
					totalItems++
				}
				extractionStats = scanflow.MergeExtractionCounts(extractionStats, itemExtractionStats)
			}
			if detection.Exclusive {
				reconcileScannedDirectory()
				return totalItems, extractionStats, nil
			}
		}
	}

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
		result, err := scanprocessor.New(s.repo, s.indexer, s.log).Process(ctx, scanprocessor.FileSingleInput(
			resource,
			tenantID,
			parentNode,
			file,
			detected,
			itemTerm,
			itemName,
			fullName,
			contentReader,
			connInfo,
			scanDepth,
		))
		if err != nil {
			s.log.Warn("保存 single 文件对象失败", "path", file.Path, "error", err)
			continue
		}
		extractionStats = scanflow.MergeExtractionCounts(extractionStats, result.Extraction)
		totalItems++
		if detected.DataType == datatype.Table && result.Item != nil {
			tableInfo := tableInfoFromMetaAttributes(result.Item.Attributes)
			if tableInfo != nil && len(tableInfo.Fields) > 0 {
				s.log.Info("识别到 single 文件表", "path", file.Path, "name", itemName, "format", detected.Format, "field_count", len(tableInfo.Fields))
			}
		}
	}

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
		items, subdirExtractionStats, scanErr := s.ScanDirectory(ctx, contentReader, catalogProvider, connInfo, resource, tenantID, subdir.Path, subdirNode, false, itemTerm, scanDepth, force)
		extractionStats = scanflow.MergeExtractionCounts(extractionStats, subdirExtractionStats)
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
