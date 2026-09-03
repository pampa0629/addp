package scanprocessor

import (
	"context"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

func (p Processor) enrichDeep(ctx context.Context, input *input, attrs models.JSONMap) (models.JSONMap, error) {
	enrichedAttrs, err := p.enrichKnownMultiTable(ctx, input, attrs)
	if err != nil {
		return nil, err
	}
	attrs = enrichedAttrs

	enriched, _, err := metaenrich.EnrichResourceAttributes(ctx, attrs, metaenrich.ResourceAttributesInput{
		ContentReader:        input.ContentReader,
		ConnInfo:             input.ConnInfo,
		EngineID:             input.EngineID,
		Item:                 input.Detected,
		PhysicalPath:         input.PhysicalPath,
		SizeBytes:            input.SizeBytes,
		IncludeAccessIndex:   input.IncludeAccessIndex,
		EngineCatalogPathFor: input.EngineCatalogPathFor,
		FormatDetector:       p.formatDetector,
		ContainerInspector:   p.containerInspector,
		SourceEngine:         input.Resource,
		TenantID:             input.TenantID,
	})
	if err != nil {
		if input.StrictDeepEnrich {
			return nil, err
		}
		p.warn("提取资源深度属性失败，保留基础属性", input, err)
	}
	if enriched != nil {
		input.Detected = enriched
	}
	return attrs, nil
}

func (p Processor) enrichKnownMultiTable(ctx context.Context, input *input, attrs models.JSONMap) (models.JSONMap, error) {
	if input.Detected.Layout != format.LayoutMulti || input.Detected.DataType != datatype.Table {
		return attrs, nil
	}
	clearStaleKnownMultiTableAccessIndex(attrs, input.Detected)
	enriched, _, err := metaitem.EnrichKnownMultiTableItem(ctx, input.ContentReader, input.ConnInfo, input.EngineID, input.EngineCatalogPathFor, input.Detected)
	if err != nil {
		if input.StrictDeepEnrich {
			return nil, err
		}
		p.warn("提取 multi table 深度属性失败，保留基础属性", input, err)
	}
	if enriched == nil {
		return attrs, nil
	}
	input.Detected = enriched
	if len(enriched.Attributes) > 0 {
		metaattr.MergeStandardAttributes(attrs, enriched.Attributes)
	}
	return attrs, nil
}

func (p Processor) extractDeepContent(ctx context.Context, input *input, attrs models.JSONMap, isDeepScan bool) (documentExtractionResult, error) {
	if !isDeepScan || input.ContentReader == nil {
		return documentExtractionResult{}, nil
	}
	if input.Detected.Layout != format.LayoutSingle {
		return documentExtractionResult{}, nil
	}
	if contentHash, err := computeContentSHA256(ctx, input.ContentReader, input.ConnInfo, input.EngineCatalogPathFor(input.PhysicalPath)); err != nil {
		if input.StrictDeepEnrich {
			return documentExtractionResult{}, err
		}
		p.warnPath("计算内容指纹失败", input.PhysicalPath, err)
	} else {
		setStorageContentHash(attrs, contentHash)
	}
	return extractCatalogDocumentText(ctx, attrs, input.ContentReader, input.ConnInfo, input.EngineID, catalogResource(input), input.Detected), nil
}

func clearStaleKnownMultiTableAccessIndex(attrs map[string]interface{}, item *metaitem.DetectedItem) {
	if attrs == nil || item == nil {
		return
	}
	if item.Layout != format.LayoutMulti || item.DataType != datatype.Table {
		return
	}
	metaattr.RemoveAccessIndexTable(attrs)
	metaattr.RemoveAccessIndexTable(item.Attributes)
}
