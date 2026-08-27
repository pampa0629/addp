package service

import (
	"context"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/metatext"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanresource"
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

func (s *IndexerService) IndexCatalogContent(ctx context.Context, resource *commonModels.Engine, tenantID, engineID uint, catalogResource scanresource.StorageResource, relativePath, fullName string, item *models.MetaItem, extractedText string) bool {
	if s.contentIndex == nil || resource == nil || item == nil {
		return false
	}

	attributes := copyJSONMap(item.Attributes)
	metadata := normalizeContentMap(attributes)
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
	dataItemType := strings.TrimSpace(item.ItemType)
	if dataItemType == "" {
		dataItemType = "item"
	}

	document := commonClient.ManagerContentDocument{
		DocumentID:     item.Fingerprint,
		ContentHash:    stringFromStandardAttributes(metadata, "storage", "content_hash"),
		Locator:        metaItemLocator(engineID, resource.EngineType, dataItemType, fullName, &item.ID),
		EngineID:       engineID,
		EngineName:     resource.Name,
		EngineType:     resource.EngineType,
		DataItemType:   dataItemType,
		Name:           item.Name,
		FullName:       fullName,
		Bucket:         catalogResource.RootName,
		Path:           dir,
		Metadata:       metadata,
		SizeBytes:      item.SizeBytes,
		DataUpdatedAt:  catalogResource.LastModified,
		Content:        truncatedContent,
		ContentPreview: contentPreview,
		ProjectionTime: time.Now().UTC(),
	}

	if len(tags) > 0 {
		document.Tags = tags
	}
	document.ContentType = commonJSON.String(metadata, "storage", "content_type")

	if value := stringFromStandardAttributes(metadata, "item", "format"); value != "" {
		document.DocumentType = value
	}
	if value := stringFromStandardAttributes(metadata, "type_info.document", "title"); value != "" {
		document.Title = value
	}
	if value := stringFromStandardAttributes(metadata, "format_info."+document.DocumentType, "author"); value != "" {
		document.Author = value
	}
	if keywords := stringSliceFromStandardAttributes(metadata, "capabilities.extraction", "keywords"); len(keywords) > 0 {
		document.Keywords = keywords
	}
	if wc := intFromStandardAttributes(metadata, "type_info.document", "word_count"); wc > 0 {
		document.WordCount = wc
	}
	if pc := intFromStandardAttributes(metadata, "type_info.document", "page_count"); pc > 0 {
		document.PageCount = pc
	}
	if created := timeFromStandardAttributes(metadata, "capabilities.extraction", "created_date"); created != nil {
		document.CreatedDate = created
	}
	if modified := timeFromStandardAttributes(metadata, "capabilities.extraction", "modified_date"); modified != nil {
		document.ModifiedDate = modified
	}

	if err := s.contentIndex.WithTenantID(tenantID).UpsertDocument(ctx, document); err != nil {
		s.log.Warn("提交 Manager 内容投影失败", "fingerprint", item.Fingerprint, "root", catalogResource.RootName, "path", catalogResource.Path, "error", err)
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
