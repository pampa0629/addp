package scanruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
)

// scanDirectory 递归扫描目录，对每个目录运行 item resolver 链。
func (s *FilesystemCatalogRuntime) scanDirectory(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	catalogProvider plugin.EngineCatalogProvider,
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
	files, subdirs, ignoredEntries, err := s.listDirectoryWithIgnored(ctx, resource, catalogProvider, connInfo, dirPath)
	if err != nil {
		return 0, scanflow.ExtractionCounts{}, fmt.Errorf("failed to list directory %s: %w", dirPath, err)
	}

	totalItems := 0
	extractionStats := scanflow.ExtractionCounts{}
	failures := &scanflow.FailedTargetCollector{}

	claimedPaths := metaitem.ResourceClaimSet{}
	scannedFileFullNames := make(map[string]bool, len(files))
	for _, file := range files {
		scannedFileFullNames[metapath.JoinFSPath(parentNode.FullName, file.Name)] = true
	}
	ignoredFullNames := make(map[string]bool, len(ignoredEntries))
	for _, entry := range ignoredEntries {
		ignoredFullNames[metapath.JoinFSPath(parentNode.FullName, entry.Name)] = true
	}
	if err := s.repo.HardDeleteItemsByNodeFullNames(parentNode.ID, ignoredFullNames); err != nil {
		s.log.Warn("清理系统噪声文件数据项失败", "path", dirPath, "node_id", parentNode.ID, "error", err)
		failures.Add(dirPath, err)
	}
	if err := s.repo.HardDeleteChildNodesByFullNames(parentNode.ID, ignoredFullNames); err != nil {
		s.log.Warn("清理系统噪声目录节点失败", "path", dirPath, "node_id", parentNode.ID, "error", err)
		failures.Add(dirPath, err)
	}
	if err := s.repo.HardDeleteChildNodesByFullNames(parentNode.ID, scannedFileFullNames); err != nil {
		s.log.Warn("清理文件路径冲突节点失败", "path", dirPath, "node_id", parentNode.ID, "error", err)
		failures.Add(dirPath, err)
	}
	scannedItemFullNames := make(map[string]bool)
	scannedSubdirFullNames := make(map[string]bool)
	reconcileScannedDirectory := func() {
		if !force {
			return
		}
		if err := s.repo.HardDeleteItemsByNodeExceptFullNames(parentNode.ID, scannedItemFullNames); err != nil {
			s.log.Warn("清理已消失的文件数据项失败", "path", dirPath, "node_id", parentNode.ID, "error", err)
			failures.Add(dirPath, err)
		}
		if err := s.repo.HardDeleteChildNodesExceptFullNames(parentNode.ID, scannedSubdirFullNames); err != nil {
			s.log.Warn("清理已消失的子目录节点失败", "path", dirPath, "node_id", parentNode.ID, "error", err)
			failures.Add(dirPath, err)
		}
	}
	applyDetection := func(detection *metaitem.DetectionResult) {
		if detection == nil {
			return
		}
		for path, claimed := range detection.Claims {
			if claimed {
				claimedPaths[path] = true
			}
		}
		for _, detected := range detection.Items {
			if detected == nil {
				continue
			}
			persisted, fullName, itemExtractionStats, persistErr := s.persistFileCatalogDetectedItem(ctx, resource, tenantID, parentNode, dirPath, detected, itemTerm, contentReader, connInfo, scanDepth)
			if fullName != "" {
				scannedItemFullNames[fullName] = true
			}
			if persisted {
				totalItems++
			}
			extractionStats = scanflow.MergeExtractionCounts(extractionStats, itemExtractionStats)
			if persistErr != nil {
				failures.Add(fullName, persistErr)
			}
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
			failures.Add(dirPath, err)
		}
		applyDetection(detection)
	} else if isDeepScan {
		detection, err := s.resolveFileCatalogDirectoryItems(ctx, contentReader, catalogProvider, connInfo, resource, dirPath, files, subdirs)
		if err != nil {
			s.log.Warn("提取复合数据项信息失败",
				"path", dirPath,
				"error", err,
			)
			failures.Add(dirPath, err)
		}
		if detection != nil {
			applyDetection(detection)
			if detection.Exclusive {
				reconcileScannedDirectory()
				return totalItems, extractionStats, failures.Err()
			}
		}
	}

	for _, file := range files {
		if claimedPaths[file.Path] {
			continue
		}
		fullName, persisted, fileExtractionStats, fileErr := s.scanSingleFileItem(fileSingleItemScanInput{
			ctx:           ctx,
			contentReader: contentReader,
			connInfo:      connInfo,
			resource:      resource,
			tenantID:      tenantID,
			parentNode:    parentNode,
			file:          file,
			itemTerm:      itemTerm,
			scanDepth:     scanDepth,
			force:         force,
			isDeepScan:    isDeepScan,
		})
		if fullName != "" {
			scannedItemFullNames[fullName] = true
		}
		if persisted {
			extractionStats = scanflow.MergeExtractionCounts(extractionStats, fileExtractionStats)
			totalItems++
		}
		if fileErr != nil {
			failures.Add(file.Path, fileErr)
		}
	}

	for _, subdir := range subdirs {
		subdirName := subdir.Name
		subdirAttrs := scanresource.FileDirectoryNodeAttributes(subdir.Path)
		subdirFullName := metapath.JoinFSPath(parentNode.FullName, subdirName)
		scannedSubdirFullNames[subdirFullName] = true
		subdirNode, err := s.repo.UpsertNode(tenantID, resource.ID, parentNode, "dir", subdirName, &subdirFullName, subdirAttrs)
		if err != nil {
			s.log.Warn("创建子目录节点失败", "path", subdir.Path, "error", err)
			failures.Add(subdir.Path, err)
			continue
		}

		var nodeStateErr error
		if err := s.repo.ResetNodeState(subdirNode, "running"); err != nil {
			failures.Add(subdir.Path, err)
			nodeStateErr = err
		}
		items, subdirExtractionStats, scanErr := s.scanDirectory(ctx, contentReader, catalogProvider, connInfo, resource, tenantID, subdir.Path, subdirNode, false, itemTerm, scanDepth, force)
		extractionStats = scanflow.MergeExtractionCounts(extractionStats, subdirExtractionStats)
		if scanErr != nil {
			s.log.Warn("递归扫描子目录失败", "path", subdir.Path, "error", scanErr)
			failures.Add(subdir.Path, scanErr)
			nodeStateErr = scanErr
		}
		if nodeStateErr != nil {
			if err := s.repo.FinalizeNodeState(subdirNode, "failed", items, 0, nodeStateErr.Error()); err != nil {
				failures.Add(subdir.Path, err)
			}
		} else {
			if err := s.repo.FinalizeNodeStateWithDepth(subdirNode, "completed", items, 0, "", scanDepth); err != nil {
				failures.Add(subdir.Path, err)
			}
		}
		totalItems += items
	}

	reconcileScannedDirectory()
	return totalItems, extractionStats, failures.Err()
}
