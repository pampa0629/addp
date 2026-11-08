package service

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/meilisearch/meilisearch-go"
	"gorm.io/gorm"
)

// Index preparation only, never called from SearchDocuments. Preserve all
// existing technical/content fields and use the same durable delivery receipts.
// Search remains unavailable until every existing document has an ordinal.
func (s *HybridSearchService) prepareSearchOrdinalBatch(ctx context.Context) error {
	if s.ordinalReady.Load() {
		return nil
	}
	if err := s.deliveries.RequireActive(ctx, s.epoch); err != nil {
		return err
	}
	var page meilisearch.DocumentsResult
	if err := s.client.Index(s.contentIndex).GetDocumentsWithContext(ctx, &meilisearch.DocumentsQuery{
		Limit: 100, Filter: "search_ordinal NOT EXISTS OR search_ordinal <= 0", Fields: []string{"id", "document_id", "tenant_id", "engine_id"},
	}, &page); err != nil {
		return err
	}
	var documents []struct {
		ID         string `json:"id"`
		DocumentID string `json:"document_id"`
		Tenant     uint   `json:"tenant_id"`
		Engine     uint   `json:"engine_id"`
	}
	if err := page.Results.DecodeInto(&documents); err != nil {
		return err
	}
	if len(documents) == 0 {
		if page.Total != 0 {
			return ErrSearchIsolated
		}
		s.ordinalReady.Store(true)
		return nil
	}
	tenant := documents[0].Tenant
	ids := []string{}
	engines := []uint{}
	for _, doc := range documents {
		if doc.ID == "" || doc.ID != doc.DocumentID || len(doc.ID) > 128 || doc.Tenant == 0 || doc.Engine == 0 {
			return errors.New("invalid indexed identity during search ordinal preparation")
		}
		if doc.Tenant == tenant {
			ids = append(ids, doc.ID)
			engines = append(engines, doc.Engine)
		}
	}
	sort.Strings(ids)
	op := &models.ContentIndexDelivery{TenantID: int64(tenant), Kind: repository.IndexDeliveryWrite, DocumentIDs: ids, EngineIDs: engines}
	var payload []map[string]any
	if err := s.deliveries.WithTransaction(ctx, func(tx *gorm.DB) error {
		assigned, err := s.deliveries.ReserveSearchOrdinals(ctx, tx, s.epoch, ids)
		if err != nil {
			return err
		}
		for _, id := range ids {
			payload = append(payload, map[string]any{"id": id, "search_ordinal": assigned[id]})
		}
		return s.deliveries.Register(ctx, tx, s.epoch, op)
	}); err != nil {
		return err
	}
	primary := "id"
	task, submitErr := s.client.Index(s.contentIndex).UpdateDocumentsWithContext(ctx, payload, &meilisearch.DocumentOptions{PrimaryKey: &primary, TaskCustomMetadata: op.TaskCorrelation})
	if err := s.recordContentReceipt(ctx, op, task, submitErr); err != nil {
		return err
	}
	if err := s.waitContentDelivery(ctx, op); err != nil {
		return fmt.Errorf("prepare indexed search ordinals: %w", err)
	}
	return nil
}

func (s *HybridSearchService) prepareSearchOrdinals(ctx context.Context) error {
	for !s.ordinalReady.Load() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.prepareSearchOrdinalBatch(ctx); err != nil {
			return err
		}
	}
	return nil
}
