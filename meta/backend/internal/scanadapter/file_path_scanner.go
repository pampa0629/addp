package scanadapter

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
)

type FilePathRuntime interface {
	EnsureFilesystemScanRoot(tenantID uint, resource *commonModels.Engine, enginePlugin plugin.EnginePlugin, scanPath string) (*models.MetaNode, *models.MetaNode, error)
	ListDirectory(ctx context.Context, resource *commonModels.Engine, catalogProvider plugin.CatalogProvider, connInfo plugin.ConnectionInfo, dirPath string) ([]metaitem.StorageFileRef, []metaitem.StorageDirectoryRef, error)
	ScanDirectory(ctx context.Context, contentReader plugin.ContentReadableProvider, catalogProvider plugin.CatalogProvider, connInfo plugin.ConnectionInfo, resource *commonModels.Engine, tenantID uint, dirPath string, parentNode *models.MetaNode, isBucketRoot bool, itemTerm string, scanDepth string, force bool) (int, scanflow.ExtractionCounts, error)
}

func ScanFilePaths(
	ctx context.Context,
	runtime FilePathRuntime,
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
	catalogProvider, ok := enginePlugin.(plugin.CatalogProvider)
	if !ok {
		return scanflow.DispatchResult{}, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
	}
	itemTerm := scanflow.CatalogLeafTermForPlugin(enginePlugin, plugin.CatalogTermFile)
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

	result := scanflow.DispatchResult{}
	for i, rootPath := range resolvedPaths {
		rootPath = metapath.SanitizeFSPath(rootPath)
		if reporter != nil {
			displayPath := rootPath
			if displayPath == "" {
				displayPath = "/"
			}
			reporter.Message(fmt.Sprintf("扫描路径 %s", displayPath))
		}

		if _, _, err := runtime.ListDirectory(ctx, resource, catalogProvider, connInfo, rootPath); err != nil {
			continue
		}

		_, scanNode, err := runtime.EnsureFilesystemScanRoot(tenantID, resource, enginePlugin, rootPath)
		if err != nil {
			continue
		}

		_ = repo.ResetNodeState(scanNode, "running")
		result.CatalogNodes++

		items, pathExtractionStats, scanErr := runtime.ScanDirectory(ctx, contentReader, catalogProvider, connInfo, resource, tenantID, rootPath, scanNode, rootPath == "", itemTerm, scanDepth, force)
		result.Extraction = scanflow.MergeExtractionCounts(result.Extraction, pathExtractionStats)
		if scanErr != nil {
			_ = repo.FinalizeNodeState(scanNode, "failed", items, 0, scanErr.Error())
		} else {
			_ = repo.FinalizeNodeStateWithDepth(scanNode, "completed", items, 0, "", scanDepth)
		}
		result.Items += items

		if reporter != nil {
			reporter.Advance(rootPath, i+1, len(resolvedPaths), map[string]interface{}{"items": items})
		}
	}

	return result, nil
}
