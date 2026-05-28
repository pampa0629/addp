package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metatext"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scantask"
)

type catalogSingleItemProcessor struct {
	repo    *metaRepo.ScanRepository
	indexer *IndexerService
	log     *slog.Logger
}

type catalogSingleItemInput struct {
	Resource            *commonModels.Engine
	TenantID            uint
	EngineID            uint
	ParentNode          *models.MetaNode
	ItemType            string
	ItemName            string
	FullName            string
	Attributes          models.JSONMap
	Detected            *metaitem.DetectedItem
	ContentReader       plugin.ContentReadableProvider
	ConnInfo            plugin.ConnectionInfo
	CatalogPath         plugin.CatalogPath
	CatalogPathFor      func(string) plugin.CatalogPath
	PhysicalPath        string
	IndexRootName       string
	IndexPath           string
	IndexRelativePath   string
	SizeBytes           int64
	DataUpdatedAt       *time.Time
	ScanDepth           string
	IncludeContentIndex bool
}

type catalogSingleItemResult struct {
	Item       *models.MetaItem
	Extraction scantask.ExtractionCounts
}

type documentExtractionResult struct {
	Text   string
	Counts scantask.ExtractionCounts
}

func mergeExtractionCounts(left, right scantask.ExtractionCounts) scantask.ExtractionCounts {
	left.Documents += right.Documents
	left.Extracted += right.Extracted
	left.Unsupported += right.Unsupported
	left.Failed += right.Failed
	left.Indexed += right.Indexed
	left.IndexFailed += right.IndexFailed
	return left
}

func (p catalogSingleItemProcessor) Process(ctx context.Context, input catalogSingleItemInput) (catalogSingleItemResult, error) {
	if input.Resource == nil {
		return catalogSingleItemResult{}, fmt.Errorf("resource is nil")
	}
	if input.ParentNode == nil {
		return catalogSingleItemResult{}, fmt.Errorf("parent node is nil")
	}
	if input.Detected == nil {
		return catalogSingleItemResult{}, fmt.Errorf("detected item is nil")
	}
	attrs := metaattr.JSONMap(input.Attributes)
	if attrs == nil {
		attrs = metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(input.Detected)))
	}
	metaattr.SetStorage(attrs, "physical_path", input.PhysicalPath)
	if input.IndexRootName != "" {
		metaattr.SetStorage(attrs, "bucket", input.IndexRootName)
	}
	dir, name := splitCatalogItemPath(input.IndexPath)
	if dir != "" {
		metaattr.SetStorage(attrs, "path", dir)
	}
	if name != "" {
		metaattr.SetStorage(attrs, "name", name)
	}
	if input.DataUpdatedAt != nil {
		metaattr.SetStorage(attrs, "last_modified_at", input.DataUpdatedAt)
	}
	if input.CatalogPathFor == nil {
		path := input.CatalogPath
		input.CatalogPathFor = func(string) plugin.CatalogPath {
			return path
		}
	}
	isDeepScan := strings.EqualFold(input.ScanDepth, "deep")
	if isDeepScan {
		_, _, err := metaenrich.EnrichResourceAttributes(ctx, attrs, metaenrich.ResourceAttributesInput{
			ContentReader:       input.ContentReader,
			ConnInfo:            input.ConnInfo,
			EngineID:            input.EngineID,
			Item:                input.Detected,
			PhysicalPath:        input.PhysicalPath,
			SizeBytes:           input.SizeBytes,
			IncludeContentIndex: input.IncludeContentIndex,
			CatalogPathFor:      input.CatalogPathFor,
		})
		if err != nil && p.log != nil {
			p.log.Warn("提取 single 资源深度属性失败，保留基础属性", "path", input.PhysicalPath, "format", input.Detected.Format, "error", err)
		}
	} else {
		metaitem.ApplyContainerSummary(attrs, input.Detected)
	}

	extraction := documentExtractionResult{}
	if isDeepScan && input.ContentReader != nil {
		if contentHash, err := computeContentSHA256(ctx, input.ContentReader, input.ConnInfo, input.CatalogPathFor(input.PhysicalPath)); err != nil {
			if p.log != nil {
				p.log.Warn("计算内容指纹失败", "path", input.PhysicalPath, "error", err)
			}
		} else {
			setStorageContentHash(attrs, contentHash)
		}
		extraction = extractCatalogDocumentText(ctx, attrs, input.ContentReader, input.ConnInfo, input.EngineID, metacatalog.StorageResource{
			RootName:     input.IndexRootName,
			Path:         input.IndexPath,
			FullPath:     input.FullName,
			NodeType:     input.ItemType,
			Format:       input.Detected.Format,
			SizeBytes:    input.SizeBytes,
			ObjectCount:  1,
			LastModified: input.DataUpdatedAt,
			CatalogPath:  input.CatalogPathFor(input.PhysicalPath),
		}, input.Detected)
	}

	rowCount := itemRowCountFromAttributes(attrs)
	item, err := p.repo.UpsertItemWithDepth(
		input.TenantID,
		input.EngineID,
		input.ParentNode,
		input.ItemType,
		input.ItemName,
		input.FullName,
		attrs,
		rowCount,
		&input.SizeBytes,
		input.DataUpdatedAt,
		input.ScanDepth,
	)
	if err != nil {
		return catalogSingleItemResult{}, err
	}

	counts := extraction.Counts
	if isDeepScan && p.indexer != nil {
		indexed := p.indexer.IndexCatalogAsset(input.Resource, input.TenantID, input.EngineID, metacatalog.StorageResource{
			RootName:     input.IndexRootName,
			Path:         input.IndexPath,
			FullPath:     input.FullName,
			NodeType:     input.ItemType,
			Format:       input.Detected.Format,
			SizeBytes:    input.SizeBytes,
			ObjectCount:  1,
			LastModified: input.DataUpdatedAt,
			CatalogPath:  input.CatalogPathFor(input.PhysicalPath),
		}, input.IndexRelativePath, input.FullName, item, extraction.Text)
		if extraction.Text != "" {
			if indexed {
				counts.Indexed++
			} else {
				counts.IndexFailed++
			}
		}
	}

	return catalogSingleItemResult{Item: item, Extraction: counts}, nil
}

func catalogItemProcessor(repo *metaRepo.ScanRepository, indexer *IndexerService, log *slog.Logger) catalogSingleItemProcessor {
	return catalogSingleItemProcessor{repo: repo, indexer: indexer, log: log}
}

func splitCatalogItemPath(value string) (dir, name string) {
	value = strings.Trim(value, "/")
	if value == "" {
		return "", ""
	}
	idx := strings.LastIndex(value, "/")
	if idx < 0 {
		return "", value
	}
	return value[:idx+1], value[idx+1:]
}

func extractCatalogDocumentText(
	ctx context.Context,
	attrs models.JSONMap,
	readableProvider plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	catalogResource metacatalog.StorageResource,
	item *metaitem.DetectedItem,
) documentExtractionResult {
	result := documentExtractionResult{}
	if attrs == nil || readableProvider == nil || item == nil || item.DataType != datatype.DataTypeDocument {
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
	rc, err := readableProvider.OpenContent(ctx, connInfo, catalogResource.CatalogPath, plugin.ReadOptions{})
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
	metaattr.SetExtraction(attrs, "index_ref", "meilisearch:assets:"+itemFingerprintForExtraction(engineID, catalogResource))
	metaattr.UpsertNested(attrs, "type_info", "document", map[string]interface{}{"text_extracted": true})
	result.Text = text
	result.Counts.Extracted = 1
	return result
}
