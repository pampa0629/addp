package scanruntime

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
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
	files []metaitem.StorageFileRef,
	itemTerm string,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	scanDepth string,
) (bool, string, scanflow.ExtractionCounts, error) {
	itemPlan, ok := scanresource.PlanFileDetectedItem(resource.ID, dirPath, detected, itemTerm)
	if !ok {
		return false, "", scanflow.ExtractionCounts{}, nil
	}
	input := scanprocessor.FileDetectedInput(
		resource,
		tenantID,
		parentNode,
		itemPlan,
		detected,
		contentReader,
		connInfo,
		scanDepth,
	)
	if detected.Layout == format.LayoutSingle {
		found := false
		for _, file := range files {
			if file.Path != itemPlan.FullName {
				continue
			}
			input = scanprocessor.FileSingleInput(resource, tenantID, parentNode, file, detected, itemTerm, itemPlan.ItemName, itemPlan.FullName, contentReader, connInfo, scanDepth)
			found = true
			break
		}
		if !found {
			return false, itemPlan.FullName, scanflow.ExtractionCounts{}, fmt.Errorf("single file source facts missing for %s", itemPlan.FullName)
		}
	}
	result, err := scanprocessor.New(s.repo, s.indexer, s.log).WithContainerInspector(s.containerInspector).Process(ctx, input)
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
