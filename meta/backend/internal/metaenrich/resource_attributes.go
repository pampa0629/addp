package metaenrich

import (
	"context"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

type ResourceAttributesInput struct {
	ContentReader       plugin.ContentReadableProvider
	ConnInfo            plugin.ConnectionInfo
	EngineID            uint
	Item                *metaitem.DetectedItem
	PhysicalPath        string
	SizeBytes           int64
	IncludeContentIndex bool
	CatalogPathFor      func(string) plugin.CatalogPath
}

func EnrichResourceAttributes(ctx context.Context, attrs models.JSONMap, input ResourceAttributesInput) (*metaitem.DetectedItem, []datatype.FieldInfo, error) {
	item := input.Item
	if attrs == nil || item == nil {
		return item, nil, nil
	}

	canReadContent := input.ContentReader != nil && input.CatalogPathFor != nil && input.PhysicalPath != ""
	if item.Layout == dataitem.LayoutSingle && canReadContent {
		enriched, ok, err := EnrichSingleTableFileItem(
			ctx,
			input.ContentReader,
			input.ConnInfo,
			input.EngineID,
			item,
			input.PhysicalPath,
			input.SizeBytes,
			input.IncludeContentIndex,
			input.CatalogPathFor,
		)
		if err != nil {
			return item, nil, err
		}
		if ok && enriched != nil {
			item = enriched
			metaattr.MergeDataItemAttributes(attrs, metaitem.AttributeInput(enriched))
		}
	}

	if len(item.Fields) > 0 {
		metaattr.SetTableFields(attrs, item.Fields)
	}

	metaitem.ApplyContainerSummary(attrs, item)
	if item.DataType == dataitem.DataTypeContainer && canReadContent {
		reader, err := input.ContentReader.OpenContent(ctx, input.ConnInfo, input.CatalogPathFor(input.PhysicalPath), plugin.ReadOptions{})
		if err != nil {
			return item, item.Fields, nil
		}
		defer reader.Close()
		_ = EnrichContainerChildren(ctx, attrs, item, reader)
	}

	return item, item.Fields, nil
}
