package scanruntime

import (
	"context"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanprocessor"
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
) (bool, string, scanflow.ExtractionCounts) {
	itemPlan, ok := metacatalog.PlanFileCatalogDetectedItem(resource.ID, dirPath, detected, itemTerm)
	if !ok {
		return false, "", scanflow.ExtractionCounts{}
	}
	result, err := scanprocessor.New(s.repo, s.indexer, s.log).WithCADInspector(s.cadInspector).Process(ctx, scanprocessor.FileDetectedInput(
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
		return false, itemPlan.FullName, result.Extraction
	}
	s.log.Info("识别到复合数据项",
		"path", dirPath,
		"full_name", itemPlan.FullName,
		"item_type", itemPlan.ItemType,
		"layout", detected.Layout,
		"data_type", detected.DataType,
		"name", itemPlan.ItemName,
	)
	return true, itemPlan.FullName, result.Extraction
}
