package scanruntime

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
)

func scanFilePaths(
	ctx context.Context,
	runtime *FilesystemCatalogRuntime,
	repo *metaRepo.ScanRepository,
	resource *commonModels.Engine,
	tenantID uint,
	paths []string,
	scanDepth string,
	force bool,
	reporter scanflow.ProgressReporter,
) (scanflow.DispatchResult, error) {
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return scanflow.DispatchResult{}, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}
	catalogProvider, ok := enginePlugin.(plugin.EngineCatalogProvider)
	if !ok {
		return scanflow.DispatchResult{}, fmt.Errorf("engine %s does not implement EngineCatalogProvider", resource.EngineType)
	}
	itemTerm := scanflow.EngineCatalogLeafTermForPlugin(enginePlugin, plugin.EngineCatalogTermFile)
	contentReader, ok := enginePlugin.(plugin.ContentReadableProvider)
	if !ok {
		return scanflow.DispatchResult{}, fmt.Errorf("engine %s does not implement ContentReadableProvider", resource.EngineType)
	}

	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)
	if len(paths) == 0 {
		paths = []string{""}
	}

	resolvedPaths, err := scanflow.ResolveCatalogScanPaths(ctx, "未检测到可扫描的路径", paths, nil, nil, reporter)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}

	for i := range resolvedPaths {
		resolvedPaths[i] = metapath.SanitizeFSPath(resolvedPaths[i])
	}
	ctx, progress := scanflow.WithFileProgress(ctx, reporter)
	plan, err := runtime.prepareFileScan(ctx, contentReader, catalogProvider, connInfo, resource, resolvedPaths, scanDepth)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}
	defer progress.Update(func(p *models.ScanProgress) {
		p.Phase = "finalizing"
		p.Operation = ""
		p.CurrentBytesRead = 0
		p.CurrentBytesTotal = 0
	})
	result := scanflow.DispatchResult{}
	failures := &scanflow.FailedTargetCollector{}
	for i, rootPath := range resolvedPaths {
		rootPath = metapath.SanitizeFSPath(rootPath)
		if reporter != nil {
			displayPath := rootPath
			if displayPath == "" {
				displayPath = "/"
			}
			reporter.Message(fmt.Sprintf("扫描路径 %s", displayPath))
		}

		if d := plan.directories[rootPath]; d.listingErr != nil {
			failures.Add(rootPath, d.listingErr)
			continue
		}

		_, scanNode, err := runtime.ensureFilesystemScanRoot(tenantID, resource, enginePlugin, rootPath)
		if err != nil {
			failures.Add(rootPath, err)
			continue
		}

		var nodeStateErr error
		if err := repo.ResetNodeState(scanNode, "running"); err != nil {
			failures.Add(rootPath, err)
			nodeStateErr = err
		}
		result.CatalogNodes++

		items, pathExtractionStats, scanErr := runtime.scanPreparedDirectory(ctx, contentReader, catalogProvider, connInfo, resource, tenantID, rootPath, scanNode, itemTerm, scanDepth, force, plan)
		result.Extraction = scanflow.MergeExtractionCounts(result.Extraction, pathExtractionStats)
		if scanErr != nil {
			failures.Add(rootPath, scanErr)
			nodeStateErr = scanErr
		}
		if nodeStateErr != nil {
			if err := repo.FinalizeNodeState(scanNode, "failed", nodeStateErr.Error()); err != nil {
				failures.Add(rootPath, err)
			}
		} else {
			if err := repo.FinalizeNodeStateWithDepth(scanNode, "completed", "", scanDepth); err != nil {
				failures.Add(rootPath, err)
			}
		}
		result.Items += items

		if reporter != nil {
			reporter.Advance(rootPath, i+1, len(resolvedPaths), map[string]interface{}{"items": items})
		}
	}

	return result, failures.Err()
}
