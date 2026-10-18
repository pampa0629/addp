package scanprocessor

import (
	"context"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

func (p Processor) enrichDeep(ctx context.Context, input *input, attrs models.JSONMap) (models.JSONMap, error) {
	originalReader := input.ContentReader
	source, err := readDelimitedIndexSource(ctx, input, originalReader)
	if err != nil {
		return nil, err
	}
	if source != nil {
		input.SizeBytes = *source.Storage.SizeBytes
		input.DataUpdatedAt = source.UpdatedAt
		metaattr.SetStorage(attrs, "total_size", input.SizeBytes)
		metaattr.SetStorage(attrs, "etag", source.Storage.ETag)
		metaattr.SetStorage(attrs, "last_modified_at", source.UpdatedAt)
	}
	input.ContentReader = observeMetadataRanges(originalReader)
	defer func() { input.ContentReader = originalReader }()

	if input.Detected.Layout == format.LayoutSingle && (input.Detected.Format == "csv" || input.Detected.Format == "tsv") {
		// Successful stored facts are the authority for omitted read settings.
		// A directory scan's fresh candidate must not reset an existing false mode.
		known := attrs
		if input.Encoding == "" || input.HasHeader == nil {
			existing, found, err := p.repo.FindItemByFullName(input.TenantID, input.EngineID, input.FullName)
			if err != nil {
				return nil, err
			}
			if found && commonJSON.String(existing.Attributes, "item", "format") == input.Detected.Format {
				known = existing.Attributes
			}
		}
		if input.Encoding == "" {
			input.Encoding = commonJSON.String(known, "format_info."+input.Detected.Format, "encoding")
		}
		if input.HasHeader == nil {
			input.HasHeader, err = format.HeaderModeFromOptions(commonJSON.Section(known, "type_info.table.native"))
			if err != nil {
				return nil, err
			}
		}
	}

	enrichedAttrs, err := p.enrichKnownMultiTable(ctx, input, attrs)
	if err != nil {
		return nil, err
	}
	attrs = enrichedAttrs

	enriched, _, err := metaenrich.EnrichResourceAttributes(ctx, attrs, metaenrich.ResourceAttributesInput{
		Encoding:             input.Encoding,
		HasHeader:            input.HasHeader,
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
		if input.StrictDeepEnrich || input.Encoding != "" || input.HasHeader != nil || source != nil {
			return nil, err
		}
		p.warn("提取资源深度属性失败，保留基础属性", input, err)
	}
	if enriched != nil {
		input.Detected = enriched
	}
	if err := bindDelimitedIndexSource(ctx, input, attrs, originalReader, source); err != nil {
		return nil, err
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
	progress := scanflow.FileProgressFrom(ctx)
	progress.Operation("hashing", input.PhysicalPath)
	progress.Update(func(p *models.ScanProgress) { p.CurrentBytesTotal = max(int64(0), input.SizeBytes) })
	if contentHash, err := computeContentSHA256(ctx, input.ContentReader, input.ConnInfo, input.EngineCatalogPathFor(input.PhysicalPath)); err != nil {
		if input.StrictDeepEnrich {
			return documentExtractionResult{}, err
		}
		p.warnPath("计算内容指纹失败", input.PhysicalPath, err)
	} else {
		setStorageContentHash(attrs, contentHash)
	}
	progress.Operation("extracting", input.PhysicalPath)
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
