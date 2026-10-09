package service

import (
	"context"
	"time"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatext"
	"github.com/addp/meta/internal/models"
)

func (s *IndexerService) IndexCatalogContent(ctx context.Context, resource *commonModels.Engine, tenantID uint, item *models.MetaItem, extractedText string, textTruncated bool) bool {
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

	document := commonClient.ManagerContentDocument{DocumentID: item.Fingerprint, EngineID: resource.ID, DataItemType: item.ItemType, Name: item.Name, ProjectionTime: time.Now().UTC()}
	document.PayloadKind = commonClient.ManagerContentPayloadExtractedContent
	document.ContentHash = stringFromStandardAttributes(metadata, "storage", "content_hash")
	document.Content = truncatedContent
	document.ContentPreview = contentPreview
	document.ContentTruncated = textTruncated

	if len(tags) > 0 {
		document.Tags = tags
	}
	if value := stringFromStandardAttributes(metadata, "type_info.document", "title"); value != "" {
		document.Title = value
	}
	if value := stringFromStandardAttributes(metadata, "format_info."+stringFromStandardAttributes(metadata, "item", "format"), "author"); value != "" {
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
		s.log.Warn("提交 Manager 内容投影失败", "fingerprint", item.Fingerprint, "full_name", item.FullName, "error", err)
		return false
	}
	return true
}
