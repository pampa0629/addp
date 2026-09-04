package scanruntime

import (
	"context"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanprocessor"
	"github.com/addp/meta/internal/scanresource"
)

func (s *FilesystemCatalogRuntime) persistFileCatalogDetectedItem(
	ctx context.Context,
	resource *commonModels.Engine,
	tenantID uint,
	parentNode *models.MetaNode,
	dirPath string,
	detected *metaitem.DetectedItem,
	itemTerm string,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	scanDepth string,
) (bool, string, scanflow.ExtractionCounts, error) {
	itemPlan, ok := scanresource.PlanFileDetectedItem(resource.ID, dirPath, detected, itemTerm)
	if !ok {
		return false, "", scanflow.ExtractionCounts{}, nil
	}
	result, err := scanprocessor.New(s.repo, s.indexer, s.log).WithContainerInspector(s.containerInspector).Process(ctx, scanprocessor.FileDetectedInput(
		resource,
		tenantID,
		parentNode,
		itemPlan,
		detected,
		contentReader,
		connInfo,
		scanDepth,
	))
	if err != nil {
		s.log.Warn("保存复合数据项失败",
			"path", dirPath,
			"item_type", itemPlan.ItemType,
			"full_name", itemPlan.FullName,
			"error", err,
		)
		return false, itemPlan.FullName, result.Extraction, err
	}
	s.log.Info("识别到复合数据项",
		"path", dirPath,
		"full_name", itemPlan.FullName,
		"item_type", itemPlan.ItemType,
		"layout", detected.Layout,
		"data_type", detected.DataType,
		"name", itemPlan.ItemName,
	)
	return true, itemPlan.FullName, result.Extraction, nil
}
