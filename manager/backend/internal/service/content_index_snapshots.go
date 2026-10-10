package service

import (
	"context"
	"errors"
	"sort"

	commonClient "github.com/addp/common/client"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/meilisearch/meilisearch-go"
)

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

// Read only the already indexed technical projection, not content or Metadata.
// Replacement drops all other fields, including unknown historical properties.
func (s *HybridSearchService) readContentTechnicalSnapshot(ctx context.Context, op *models.ContentIndexDelivery) ([]map[string]interface{}, error) {
	if op == nil || op.TenantID <= 0 || op.DocumentID == "" {
		return nil, repository.ErrContentIndexIsolated
	}
	fields := make([]string, 0)
	for key := range contentSnapshotPayload(uint(op.TenantID), commonClient.ManagerContentDocument{PayloadKind: commonClient.ManagerContentPayloadTechnicalMetadata}) {
		fields = append(fields, key)
	}
	sort.Strings(fields)
	var indexed struct {
		commonClient.ManagerContentDocument
		ID       string `json:"id"`
		TenantID int64  `json:"tenant_id"`
	}
	err := s.client.Index(s.contentIndex).GetDocumentWithContext(ctx, op.DocumentID, &meilisearch.DocumentQuery{Fields: fields}, &indexed)
	if err != nil {
		var apiErr *meilisearch.Error
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 && (apiErr.MeilisearchApiError.Code == "document_not_found" || apiErr.MeilisearchApiError.Code == "index_not_found") {
			return []map[string]interface{}{}, nil
		}
		return nil, repository.ErrContentIndexIsolated
	}
	if indexed.ID != op.DocumentID || indexed.DocumentID != op.DocumentID || indexed.TenantID != op.TenantID || indexed.EngineID == 0 {
		return nil, repository.ErrContentIndexIsolated
	}
	if indexed.ProjectionTime.IsZero() {
		// A content-only document has no technical snapshot to preserve.
		return []map[string]interface{}{{"id": indexed.ID, "document_id": indexed.DocumentID, "tenant_id": indexed.TenantID, "engine_id": indexed.EngineID}}, nil
	}
	indexed.PayloadKind = commonClient.ManagerContentPayloadTechnicalMetadata
	if indexed.Validate() != nil {
		return nil, repository.ErrContentIndexIsolated
	}
	return []map[string]interface{}{contentSnapshotPayload(uint(op.TenantID), indexed.ManagerContentDocument)}, nil
}
