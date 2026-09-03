package metaenrich

import (
	"context"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

type ResourceAttributesInput struct {
	ContentReader        plugin.ContentReadableProvider
	ConnInfo             plugin.ConnectionInfo
	EngineID             uint
	Item                 *metaitem.DetectedItem
	PhysicalPath         string
	SizeBytes            int64
	IncludeAccessIndex   bool
	EngineCatalogPathFor func(string) plugin.EngineCatalogPath
	FormatDetector       RuntimeFormatDetector
	ContainerInspector   ContainerInspector
	SourceEngine         *commonModels.Engine
	TenantID             uint
}

func EnrichResourceAttributes(ctx context.Context, attrs models.JSONMap, input ResourceAttributesInput) (*metaitem.DetectedItem, []datatype.FieldInfo, error) {
	item := input.Item
	if attrs == nil || item == nil {
		return item, nil, nil
	}
	if err := RefineRuntimeFormat(ctx, attrs, input.FormatDetector, input.SourceEngine, input.TenantID, item, input.PhysicalPath); err != nil {
		return item, nil, err
	}

	canReadContent := input.ContentReader != nil && input.EngineCatalogPathFor != nil && input.PhysicalPath != ""
	if item.Layout == format.LayoutSingle && canReadContent {
		beforeDataType := item.DataType
		beforeFormat := item.Format
		enriched, ok, err := EnrichSingleTableFileItem(
			ctx,
			input.ContentReader,
			input.ConnInfo,
			input.EngineID,
			item,
			input.PhysicalPath,
			input.SizeBytes,
			input.IncludeAccessIndex,
			input.EngineCatalogPathFor,
		)
		if err != nil {
			return item, nil, err
		}
		if ok && enriched != nil {
			item = enriched
			metaattr.MergeDataItemAttributes(attrs, metaitem.AttributeInput(enriched))
		} else if item.DataType != beforeDataType || item.Format != beforeFormat {
			metaattr.MergeDataItemAttributes(attrs, metaitem.AttributeInput(item))
		}
	}
	if len(item.Fields) > 0 {
		metaattr.SetTableFields(attrs, item.Fields)
	}
	if canReadContent {
		if err := EnrichSingleCADItem(ctx, attrs, input.ContentReader, input.ConnInfo, input.EngineID, item, input.PhysicalPath, input.SizeBytes, input.EngineCatalogPathFor); err != nil {
			return item, item.Fields, err
		}
	}

	if canReadContent && (item.Layout == format.LayoutSingle || item.Layout == format.LayoutMulti) {
		if err := EnrichSingleDocumentItem(ctx, attrs, input.ContentReader, input.ConnInfo, input.EngineID, item, input.PhysicalPath, input.EngineCatalogPathFor); err != nil {
			return item, item.Fields, err
		}
		if err := EnrichSingleMediaItem(ctx, attrs, input.ContentReader, input.ConnInfo, input.EngineID, item, input.PhysicalPath, input.EngineCatalogPathFor); err != nil {
			return item, item.Fields, err
		}
		if err := EnrichSingleGaussianSplatItem(ctx, attrs, input.ContentReader, input.ConnInfo, input.EngineID, item, input.PhysicalPath, input.SizeBytes, input.EngineCatalogPathFor); err != nil {
			return item, item.Fields, err
		}
		if err := EnrichSinglePointCloudItem(ctx, attrs, input.ContentReader, input.ConnInfo, input.EngineID, item, input.PhysicalPath, input.EngineCatalogPathFor); err != nil {
			return item, item.Fields, err
		}
		if err := EnrichSingleModel3DItem(ctx, attrs, input.ContentReader, input.ConnInfo, input.EngineID, item, input.PhysicalPath, input.EngineCatalogPathFor); err != nil {
			return item, item.Fields, err
		}
	}
	metaitem.ApplyContainerSummary(attrs, item)
	if handled, err := EnrichRuntimeContainerItem(ctx, attrs, input.ContainerInspector, input.SourceEngine, input.TenantID, item, input.PhysicalPath); err != nil {
		return item, item.Fields, err
	} else if handled {
		return item, item.Fields, nil
	}
	if item.DataType == datatype.Container && canReadContent {
		reader, err := input.ContentReader.OpenContent(ctx, input.ConnInfo, input.EngineCatalogPathFor(input.PhysicalPath), plugin.ReadOptions{})
		if err != nil {
			return item, item.Fields, nil
		}
		defer reader.Close()
		_ = EnrichContainerChildren(ctx, attrs, item, reader)
	}

	return item, item.Fields, nil
}
