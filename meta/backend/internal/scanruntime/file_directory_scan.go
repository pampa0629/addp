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

// scanPreparedDirectory consumes the shared inventory and its resolved logical items.
func (s *FilesystemCatalogRuntime) scanPreparedDirectory(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	catalogProvider plugin.EngineCatalogProvider,
	connInfo plugin.ConnectionInfo,
	resource *commonModels.Engine,
	tenantID uint,
	dirPath string,
	parentNode *models.MetaNode,
	itemTerm string,
	scanDepth string,
	force bool,
	plan *fileScanPlan,
) (int, scanflow.ExtractionCounts, error) {
	if err := ctx.Err(); err != nil {
		return 0, scanflow.ExtractionCounts{}, err
	}
	d := plan.directories[dirPath]
	if d == nil {
		return 0, scanflow.ExtractionCounts{}, fmt.Errorf("directory missing from file scan inventory: %q", dirPath)
	}
	if d.listingErr != nil {
		return 0, scanflow.ExtractionCounts{}, fmt.Errorf("failed to list directory %s: %w", dirPath, d.listingErr)
	}
	files, subdirs, ignoredEntries := d.files, d.subdirs, d.ignored

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
		if !force || failures.Err() != nil {
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
			if err := ctx.Err(); err != nil {
				failures.Add(dirPath, err)
				return
			}
			if detected == nil {
				continue
			}
			plan.progress.Operation("saving", detected.PhysicalPath)
			persisted, fullName, itemExtractionStats, persistErr := s.persistFileCatalogDetectedItem(ctx, resource, tenantID, parentNode, dirPath, detected, itemTerm, contentReader, connInfo, scanDepth)
			if fullName != "" {
				plan.progress.ItemDone(persisted && persistErr == nil)
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
	if d.detectionErr != nil {
		s.log.Warn("提取复合数据项信息失败", "path", dirPath, "error", d.detectionErr)
		failures.Add(dirPath, d.detectionErr)
	}
	applyDetection(d.detection)
	if d.detection != nil && d.detection.Exclusive {
		reconcileScannedDirectory()
		return totalItems, extractionStats, failures.Err()
	}

	for _, file := range files {
		if err := ctx.Err(); err != nil {
			failures.Add(dirPath, err)
			return totalItems, extractionStats, failures.Err()
		}
		if claimedPaths[file.Path] {
			continue
		}
		plan.progress.Operation("saving", file.Path)
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
		plan.progress.ItemDone(persisted && fileErr == nil)
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
		if err := ctx.Err(); err != nil {
			failures.Add(dirPath, err)
			return totalItems, extractionStats, failures.Err()
		}
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
		items, subdirExtractionStats, scanErr := s.scanPreparedDirectory(ctx, contentReader, catalogProvider, connInfo, resource, tenantID, subdir.Path, subdirNode, itemTerm, scanDepth, force, plan)
		extractionStats = scanflow.MergeExtractionCounts(extractionStats, subdirExtractionStats)
		if scanErr != nil {
			s.log.Warn("递归扫描子目录失败", "path", subdir.Path, "error", scanErr)
			failures.Add(subdir.Path, scanErr)
			nodeStateErr = scanErr
		}
		if nodeStateErr != nil {
			if err := s.repo.FinalizeNodeState(subdirNode, "failed", nodeStateErr.Error()); err != nil {
				failures.Add(subdir.Path, err)
			}
		} else {
			if err := s.repo.FinalizeNodeStateWithDepth(subdirNode, "completed", "", scanDepth); err != nil {
				failures.Add(subdir.Path, err)
			}
		}
		totalItems += items
	}

	reconcileScannedDirectory()
	return totalItems, extractionStats, failures.Err()
}
