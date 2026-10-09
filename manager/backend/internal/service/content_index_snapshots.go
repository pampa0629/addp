package service

import commonClient "github.com/addp/common/client"

// Each request is a complete snapshot of its owned fields. Explicit empty
// values clear stale facts; absent fields leave the other snapshot untouched.
func contentSnapshotPayload(tenantID uint, d commonClient.ManagerContentDocument) map[string]interface{} {
	p := map[string]interface{}{"id": d.DocumentID, "document_id": d.DocumentID, "tenant_id": tenantID, "engine_id": d.EngineID}
	if d.PayloadKind == commonClient.ManagerContentPayloadTechnicalMetadata {
		for k, v := range map[string]interface{}{
			"name": d.Name, "full_name": d.FullName, "data_item_type": d.DataItemType,
			"engine_name": d.EngineName, "engine_type": d.EngineType, "locator": d.Locator,
			"description": d.Description, "schema": d.Schema, "table_kind": d.TableKind,
			"fields": d.Fields, "row_count": d.RowCount, "bucket": d.Bucket, "path": d.Path,
			"size_bytes": d.SizeBytes, "content_type": d.ContentType, "document_type": d.DocumentType,
			"data_updated_at": d.DataUpdatedAt, "projection_time": d.ProjectionTime,
		} {
			p[k] = v
		}
	} else {
		for k, v := range map[string]interface{}{
			"content_hash": d.ContentHash, "content": d.Content, "content_preview": d.ContentPreview,
			"content_truncated": d.ContentTruncated, "title": d.Title, "author": d.Author,
			"keywords": d.Keywords, "tags": d.Tags, "word_count": d.WordCount, "page_count": d.PageCount,
			"created_date": d.CreatedDate, "modified_date": d.ModifiedDate, "metadata": d.Metadata,
			"content_projection_time": d.ProjectionTime,
		} {
			p[k] = v
		}
	}
	return p
}
