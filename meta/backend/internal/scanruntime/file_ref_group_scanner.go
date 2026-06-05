package scanruntime

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

func scanFileRefGroups(
	ctx context.Context,
	runtime *FilesystemCatalogRuntime,
	resource *commonModels.Engine,
	tenantID uint,
	groups []models.ScanRefGroup,
	scanDepth string,
	reporter scanflow.ProgressReporter,
) (scanflow.DispatchResult, error) {
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return scanflow.DispatchResult{}, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}
	contentReader, ok := enginePlugin.(plugin.ContentReadableProvider)
	if !ok {
		return scanflow.DispatchResult{}, fmt.Errorf("engine %s does not implement ContentReadableProvider", resource.EngineType)
	}
	itemTerm := scanflow.CatalogLeafTermForPlugin(enginePlugin, plugin.CatalogTermFile)
	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)

	result := scanflow.DispatchResult{}
	seenNodes := map[uint]bool{}

	for i, group := range groups {
		primary := scanflow.ScanRefGroupPrimaryPath(group)
		if primary == "" {
			continue
		}
		candidates := scanflow.FileRefGroupCandidateSet(resource.ID, primary, group)
		if reporter != nil {
			reporter.Message(fmt.Sprintf("扫描内容引用组 %s", primary))
		}

		_, parentNode, err := runtime.ensureFilesystemScanRoot(tenantID, resource, enginePlugin, candidates.DirPath)
		if err != nil {
			return result, err
		}
		if parentNode != nil && !seenNodes[parentNode.ID] {
			seenNodes[parentNode.ID] = true
			result.CatalogNodes++
		}

		detection, err := scanflow.ResolveContentCandidates(ctx, contentReader, connInfo, resource.ID, candidates)
		if err != nil {
			return result, err
		}
		for _, detected := range detection.Items {
			persisted, _, itemExtractionStats := runtime.persistFileCatalogDetectedItem(ctx, resource, tenantID, parentNode, candidates.DirPath, detected, itemTerm, contentReader, connInfo, scanDepth)
			if persisted {
				result.Items++
			}
			result.Extraction = scanflow.MergeExtractionCounts(result.Extraction, itemExtractionStats)
		}
		if reporter != nil {
			reporter.Advance(primary, i+1, len(groups), map[string]interface{}{"items": result.Items})
		}
	}
	return result, nil
}
