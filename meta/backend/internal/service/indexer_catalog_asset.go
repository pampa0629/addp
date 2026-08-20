package service

import (
	"context"
	"strings"

	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metatext"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/search"
)

func splitCatalogResourcePath(value string) (dir, name string) {
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

// IndexCatalogAsset 索引 catalog single item 资产到 Meilisearch（统一索引）。
func (s *IndexerService) IndexCatalogAsset(ctx context.Context, resource *commonModels.Engine, tenantID, engineID uint, catalogResource metacatalog.StorageResource, relativePath, fullName string, item *models.MetaItem, extractedText string) bool {
	if s.indexer == nil || !s.indexer.Enabled() || resource == nil || item == nil {
		return false
	}

	attributes := copyJSONMap(item.Attributes)
	metadata := search.NormalizeMap(attributes)
	if metadata == nil {
		metadata = map[string]interface{}{}
	}

	tags := extractStringSlice(metadata["tags"])
	if len(tags) > 0 {
		delete(metadata, "tags")
	}

	plainText := extractedText
	delete(metadata, "plain_text")

	truncatedContent := metatext.TruncateRunes(plainText, metatext.DocumentContentRuneLimit)
	contentPreview := metatext.PreviewText(truncatedContent, metatext.DocumentPreviewRuneLimit)

	dir, _ := splitCatalogResourcePath(catalogResource.Path)
	assetType := strings.TrimSpace(item.ItemType)
	if assetType == "" {
		assetType = "item"
	}

	assetRecord := &search.AssetRecord{
		AssetID:        item.Fingerprint,
		DocumentID:     item.Fingerprint,
		ContentHash:    stringFromStandardAttributes(metadata, "storage", "content_hash"),
		Locator:        metaItemLocator(engineID, resource.EngineType, assetType, fullName, &item.ID),
		TenantID:       tenantID,
		EngineID:       engineID,
		EngineName:     resource.Name,
		EngineType:     resource.EngineType,
		AssetType:      assetType,
		Name:           item.Name,
		FullName:       fullName,
		Bucket:         catalogResource.RootName,
		Path:           dir,
		Metadata:       metadata,
		SizeBytes:      item.SizeBytes,
		DataUpdatedAt:  catalogResource.LastModified,
		Content:        truncatedContent,
		ContentPreview: contentPreview,
	}

	if len(tags) > 0 {
		assetRecord.Tags = tags
	}
	assetRecord.ContentType = commonJSON.String(metadata, "storage", "content_type")

	if value := stringFromStandardAttributes(metadata, "item", "format"); value != "" {
		assetRecord.DocumentType = value
	}
	if value := stringFromStandardAttributes(metadata, "type_info.document", "title"); value != "" {
		assetRecord.Title = value
	}
	if value := stringFromStandardAttributes(metadata, "format_info."+assetRecord.DocumentType, "author"); value != "" {
		assetRecord.Author = value
	}
	if keywords := stringSliceFromStandardAttributes(metadata, "capabilities.extraction", "keywords"); len(keywords) > 0 {
		assetRecord.Keywords = keywords
	}
	if wc := intFromStandardAttributes(metadata, "type_info.document", "word_count"); wc > 0 {
		assetRecord.WordCount = wc
	}
	if pc := intFromStandardAttributes(metadata, "type_info.document", "page_count"); pc > 0 {
		assetRecord.PageCount = pc
	}
	if created := timeFromStandardAttributes(metadata, "capabilities.extraction", "created_date"); created != nil {
		assetRecord.CreatedDate = created
	}
	if modified := timeFromStandardAttributes(metadata, "capabilities.extraction", "modified_date"); modified != nil {
		assetRecord.ModifiedDate = modified
	}

	if err := s.indexer.IndexAsset(ctx, assetRecord); err != nil {
		s.log.Warn("索引 catalog 资产失败", "fingerprint", item.Fingerprint, "root", catalogResource.RootName, "path", catalogResource.Path, "error", err)
		return false
	}
	return true
}

func metaItemLocator(engineID uint, engineType, itemType, fullName string, itemID *uint) string {
	loc := resourcetree.LocatorFromFullName(engineID, engineType, itemType, fullName, itemID)
	if loc == nil {
		return ""
	}
	return loc.ToURI()
}
