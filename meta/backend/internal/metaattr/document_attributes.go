package metaattr

import (
	"github.com/addp/common/datatype"
	"github.com/addp/meta/internal/models"
)

func DocumentInfoAttributes(info *datatype.DocumentInfo) models.JSONMap {
	attrs := models.JSONMap{}
	if info == nil {
		return attrs
	}
	document := map[string]interface{}{}
	if info.Title != "" {
		document["title"] = info.Title
	}
	if info.Language != "" {
		document["language"] = info.Language
	}
	if info.Encoding != "" {
		document["encoding"] = info.Encoding
	}
	if info.PageCount > 0 {
		document["page_count"] = info.PageCount
	}
	if info.WordCount > 0 {
		document["word_count"] = info.WordCount
	}
	if info.SizeBytes != nil {
		document["size_bytes"] = *info.SizeBytes
	}
	if info.TextExtracted {
		document["text_extracted"] = true
	}
	UpsertNested(attrs, "type_info", "document", document)
	return attrs
}
