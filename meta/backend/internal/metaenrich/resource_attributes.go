package metaenrich

import (
	"context"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/contentadapter"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	metaErrors "github.com/addp/meta/internal/errors"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

type ResourceAttributesInput struct {
	Encoding             string
	HasHeader            *bool
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
	MediaInspector       RuntimeMediaInspector
	TableInspector       RuntimeTableInspector
	SourceEngine         *commonModels.Engine
	TenantID             uint
}

func EnrichResourceAttributes(ctx context.Context, attrs models.JSONMap, input ResourceAttributesInput) (*metaitem.DetectedItem, []datatype.FieldInfo, error) {
	item := input.Item
	if attrs == nil || item == nil {
		return item, nil, nil
	}
	if err := identifySingleFile(ctx, attrs, input); err != nil {
		return item, nil, err
	}
	if err := RefineRuntimeFormat(ctx, attrs, input.FormatDetector, input.SourceEngine, input.TenantID, item, input.PhysicalPath); err != nil {
		return item, nil, err
	}

	if (input.Encoding != "" || input.HasHeader != nil) && (item.Layout != format.LayoutSingle || item.DataType != datatype.Table || (item.Format != "csv" && item.Format != "tsv")) {
		return item, nil, metaErrors.ErrInvalidDelimitedOptions
	}
	canReadContent := input.ContentReader != nil && input.EngineCatalogPathFor != nil && input.PhysicalPath != ""
	if handled, err := EnrichRuntimeTableItem(ctx, attrs, input.TableInspector, input.SourceEngine, input.TenantID, item, input.PhysicalPath); err != nil {
		return item, nil, err
	} else if handled {
		return item, item.Fields, nil
	}
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
			input.Encoding,
			input.HasHeader,
			input.EngineCatalogPathFor,
		)
		if err != nil {
			return item, nil, err
		}
		if ok && enriched != nil {
			item = enriched
			if item.Format == "csv" || item.Format == "tsv" {
				metaattr.RemoveAccessIndexTable(attrs)
			}
			metaattr.MergeDataItemAttributes(attrs, metaitem.AttributeInput(enriched))
		} else if item.DataType != beforeDataType || item.Format != beforeFormat {
			metaattr.MergeDataItemAttributes(attrs, metaitem.AttributeInput(item))
		}
	}
	if item.Layout == format.LayoutWhole && item.DataType == datatype.Table && canReadContent {
		reader := contentadapter.NewMappedReader(input.ContentReader, input.ConnInfo,
			contentadapter.ScopePathMapper(input.EngineCatalogPathFor(input.PhysicalPath)), plugin.ReadOptions{})
		info, err := describeTableFileScope(ctx, item.Format, reader, input.PhysicalPath)
		if err != nil {
			return item, nil, err
		}
		item.Fields = tableFieldsFromDescribeResult(info)
		metaattr.MergeStandardAttributes(attrs, metaattr.TableDescribeAttributes(metaattr.TableDescribeAttributesInput{
			FormatName: item.Format, Table: info.Table, FormatInfo: info.FormatInfo,
			Spatial: info.Spatial, AccessIndex: info.AccessIndex, IncludeAccessIndex: input.IncludeAccessIndex,
		}))
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
		handled, err := EnrichRuntimeMediaItem(ctx, attrs, input.MediaInspector, input.SourceEngine, input.TenantID, item, input.PhysicalPath)
		if err != nil {
			return item, item.Fields, err
		}
		if !handled {
			if err := EnrichSingleMediaItem(ctx, attrs, input.ContentReader, input.ConnInfo, input.EngineID, item, input.PhysicalPath, input.EngineCatalogPathFor); err != nil {
				return item, item.Fields, err
			}
		}
		if err := EnrichSingleGaussianSplatItem(ctx, attrs, input.ContentReader, input.ConnInfo, input.EngineID, item, input.PhysicalPath, input.SizeBytes, input.EngineCatalogPathFor); err != nil {
			return item, item.Fields, err
		}
		if err := EnrichSinglePointCloudItem(ctx, attrs, input.ContentReader, input.ConnInfo, input.EngineID, item, input.PhysicalPath, input.EngineCatalogPathFor); err != nil {
			return item, item.Fields, err
		}
		if err := EnrichSingleModel3DItem(ctx, attrs, input.ContentReader, input.ConnInfo, input.EngineID, input.SourceEngine, item, input.PhysicalPath, input.EngineCatalogPathFor); err != nil {
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
			return item, item.Fields, err
		}
		defer reader.Close()
		if err := EnrichContainerChildren(ctx, attrs, item, reader); err != nil {
			return item, item.Fields, err
		}
	}

	return item, item.Fields, nil
}
