package scanruntime

import (
	"context"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanprocessor"
)

type fileSingleItemScanInput struct {
	ctx           context.Context
	contentReader plugin.ContentReadableProvider
	connInfo      plugin.ConnectionInfo
	resource      *commonModels.Engine
	tenantID      uint
	parentNode    *models.MetaNode
	file          metaitem.StorageFileRef
	itemTerm      string
	scanDepth     string
	force         bool
	isDeepScan    bool
}

func (s *FilesystemCatalogRuntime) scanSingleFileItem(input fileSingleItemScanInput) (string, bool, scanflow.ExtractionCounts) {
	detected := metaitem.InferSingleResourceItem(input.file)
	itemName := input.file.Name
	fullName := metapath.JoinFSPath(input.parentNode.FullName, itemName)

	existingItem, itemExists, findErr := s.repo.FindItemByFullName(input.tenantID, input.resource.ID, fullName)
	if findErr != nil {
		s.log.Warn("查询文件对象失败", "path", input.file.Path, "error", findErr)
	}
	if itemExists && !input.force && !fileItemNeedsScan(existingItem, input.file, input.isDeepScan) {
		return fullName, true, scanflow.ExtractionCounts{}
	}

	result, err := scanprocessor.New(s.repo, s.indexer, s.log).WithCADInspector(s.cadInspector).WithContainerInspector(s.containerInspector).Process(input.ctx, scanprocessor.FileSingleInput(
		input.resource,
		input.tenantID,
		input.parentNode,
		input.file,
		detected,
		input.itemTerm,
		itemName,
		fullName,
		input.contentReader,
		input.connInfo,
		input.scanDepth,
	))
	if err != nil {
		s.log.Warn("保存 single 文件对象失败", "path", input.file.Path, "error", err)
		return fullName, false, result.Extraction
	}

	if detected.DataType == datatype.Table && result.Item != nil {
		tableInfo := tableInfoFromMetaAttributes(result.Item.Attributes)
		if tableInfo != nil && len(tableInfo.Fields) > 0 {
			s.log.Info("识别到 single 文件表", "path", input.file.Path, "name", itemName, "format", detected.Format, "field_count", len(tableInfo.Fields))
		}
	}
	return fullName, true, result.Extraction
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
