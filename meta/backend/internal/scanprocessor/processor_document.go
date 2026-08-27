package scanprocessor

import (
	"context"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metatext"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanresource"
)

func extractCatalogDocumentText(
	ctx context.Context,
	attrs models.JSONMap,
	readableProvider plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	_ uint,
	catalogResource scanresource.StorageResource,
	item *metaitem.DetectedItem,
) documentExtractionResult {
	result := documentExtractionResult{}
	if attrs == nil || readableProvider == nil || item == nil || item.DataType != datatype.Document {
		return result
	}
	result.Counts.Documents = 1
	formatName := strings.TrimSpace(item.Format)
	if formatName == "" {
		formatName = commonJSON.String(attrs, "item", "format")
	}
	if formatName == "" {
		result.Counts.Unsupported = 1
		return result
	}
	formatType := format.NormalizeFormat(formatName)
	if formatType == format.FormatUnknown {
		metaattr.SetExtraction(attrs, "extractor_available", false)
		metaattr.SetExtraction(attrs, "text_extracted", false)
		metaattr.SetExtraction(attrs, "status", "unsupported")
		metaattr.SetExtraction(attrs, "reason", "document_format_unknown")
		result.Counts.Unsupported = 1
		return result
	}
	reader, err := format.GetDocumentTextReader(formatType)
	if err != nil {
		metaattr.SetExtraction(attrs, "extractor_available", false)
		metaattr.SetExtraction(attrs, "text_extracted", false)
		metaattr.SetExtraction(attrs, "status", "unsupported")
		metaattr.SetExtraction(attrs, "reason", "document_text_reader_unavailable")
		result.Counts.Unsupported = 1
		return result
	}
	rc, err := readableProvider.OpenContent(ctx, connInfo, catalogResource.EngineCatalogPath, plugin.ReadOptions{})
	if err != nil {
		metaattr.SetExtraction(attrs, "text_extracted", false)
		metaattr.SetExtraction(attrs, "extractor_available", true)
		metaattr.SetExtraction(attrs, "status", "failed")
		metaattr.SetExtraction(attrs, "reason", "content_open_failed")
		result.Counts.Failed = 1
		return result
	}
	defer rc.Close()

	limit := int64(metatext.DocumentContentRuneLimit)
	text, truncated, err := reader.ReadDocumentText(ctx, rc, limit, nil)
	if err != nil {
		metaattr.SetExtraction(attrs, "text_extracted", false)
		metaattr.SetExtraction(attrs, "extractor_available", true)
		metaattr.SetExtraction(attrs, "status", "failed")
		metaattr.SetExtraction(attrs, "reason", "document_text_read_failed")
		result.Counts.Failed = 1
		return result
	}
	preview := metatext.PreviewText(text, metatext.DocumentPreviewRuneLimit)
	metaattr.SetExtraction(attrs, "extractor_available", true)
	metaattr.SetExtraction(attrs, "text_extracted", true)
	metaattr.SetExtraction(attrs, "status", "completed")
	metaattr.SetExtraction(attrs, "extractor", "common_format:"+string(formatType))
	metaattr.SetExtraction(attrs, "plain_text_preview", preview)
	metaattr.SetExtraction(attrs, "text_truncated", truncated)
	result.Text = text
	result.Counts.Extracted = 1
	return result
}
