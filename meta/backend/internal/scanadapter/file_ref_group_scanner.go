package scanadapter

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

type FileRefGroupRuntime interface {
	EnsureFilesystemScanRoot(tenantID uint, resource *commonModels.Engine, enginePlugin plugin.EnginePlugin, scanPath string) (*models.MetaNode, *models.MetaNode, error)
	PersistFileCatalogDetectedItem(ctx context.Context, resource *commonModels.Engine, tenantID uint, parentNode *models.MetaNode, dirPath string, detected *metaitem.DetectedItem, itemTerm string, contentReader plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, scanDepth string) (bool, string, scanflow.ExtractionCounts)
}

func ScanFileRefGroups(
	ctx context.Context,
	runtime FileRefGroupRuntime,
	resource *commonModels.Engine,
	tenantID uint,
	groups []models.ScanRefGroup,
	scanDepth string,
	reporter scanflow.ProgressReporter,
) (int, int, scanflow.ExtractionCounts, error) {
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return 0, 0, scanflow.ExtractionCounts{}, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}
	contentReader, ok := enginePlugin.(plugin.ContentReadableProvider)
	if !ok {
		return 0, 0, scanflow.ExtractionCounts{}, fmt.Errorf("engine %s does not implement ContentReadableProvider", resource.EngineType)
	}
	itemTerm := scanflow.CatalogLeafTermForPlugin(enginePlugin, plugin.CatalogTermFile)
	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)

	totalNodes := 0
	totalItems := 0
	extractionStats := scanflow.ExtractionCounts{}
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

		_, parentNode, err := runtime.EnsureFilesystemScanRoot(tenantID, resource, enginePlugin, candidates.DirPath)
		if err != nil {
			return totalNodes, totalItems, extractionStats, err
		}
		if parentNode != nil && !seenNodes[parentNode.ID] {
			seenNodes[parentNode.ID] = true
			totalNodes++
		}

		detection, err := scanflow.ResolveContentCandidates(ctx, contentReader, connInfo, resource.ID, candidates)
		if err != nil {
			return totalNodes, totalItems, extractionStats, err
		}
		for _, detected := range detection.Items {
			persisted, _, itemExtractionStats := runtime.PersistFileCatalogDetectedItem(ctx, resource, tenantID, parentNode, candidates.DirPath, detected, itemTerm, contentReader, connInfo, scanDepth)
			if persisted {
				totalItems++
			}
			extractionStats = scanflow.MergeExtractionCounts(extractionStats, itemExtractionStats)
		}
		if reporter != nil {
			reporter.Advance(primary, i+1, len(groups), map[string]interface{}{"items": totalItems})
		}
	}
	return totalNodes, totalItems, extractionStats, nil
}
