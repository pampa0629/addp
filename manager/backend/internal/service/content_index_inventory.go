package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/addp/manager/internal/repository"
	"github.com/meilisearch/meilisearch-go"
)

var ErrContentInventoryIncomplete = errors.New("content index inventory could not be verified")

type ContentIndexIdentity struct {
	DocumentID string `json:"document_id"`
	TenantID   uint   `json:"tenant_id"`
	EngineID   uint   `json:"engine_id"`
}
type ContentIndexInventory struct {
	IndexName string
	TenantID  uint
	Guard     repository.ContentInventoryGuard
	Documents []ContentIndexIdentity
	Digest    string
}

// ReadContentIndexInventory performs no initialization, recovery or fencing.
// Two separate database observations surround HTTP reads; a transaction spanning
// those reads would conceal newly committed deliveries from the second observation.
func (s *HybridSearchService) ReadContentIndexInventory(ctx context.Context, tenantID uint) (*ContentIndexInventory, error) {
	if tenantID == 0 || !s.Enabled() || !s.initialized.Load() || s.deliveries == nil {
		return nil, ErrContentInventoryIncomplete
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	before, err := s.deliveries.ObserveInventoryGuard(ctx, s.epoch)
	if err != nil {
		return nil, err
	}
	if before.EndpointID != s.endpointID {
		return nil, ErrContentInventoryIncomplete
	}
	result, err := s.readContentIndexIdentities(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	result.Guard = before
	after, err := s.deliveries.ObserveInventoryGuard(ctx, s.epoch)
	if err != nil {
		return nil, err
	}
	if before != after {
		return nil, ErrContentInventoryIncomplete
	}
	return result, nil
}

// readContentIndexIdentities performs provider reads only. Callers supply either
// two stable outlet observations or a C-bound persistent Cleanup hold.
func (s *HybridSearchService) readContentIndexIdentities(ctx context.Context, tenantID uint) (*ContentIndexInventory, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	const pageSize int64 = 256
	const maxDocuments int64 = 100000
	result := &ContentIndexInventory{IndexName: s.contentIndex, TenantID: tenantID, Documents: []ContentIndexIdentity{}}
	seen := map[string]bool{}
	total := int64(-1)
	for offset := int64(0); ; {
		var page meilisearch.DocumentsResult
		err := s.client.Index(s.contentIndex).GetDocumentsWithContext(ctx, &meilisearch.DocumentsQuery{Offset: offset, Limit: pageSize, Fields: []string{"id", "document_id", "tenant_id", "engine_id"}, Filter: fmt.Sprintf("tenant_id = %d", tenantID)}, &page)
		if err != nil {
			return nil, ErrContentInventoryIncomplete
		}
		if page.Offset != offset || page.Limit != pageSize || page.Total < 0 || page.Total > maxDocuments || len(page.Results) > int(pageSize) || total >= 0 && total != page.Total {
			return nil, ErrContentInventoryIncomplete
		}
		total = page.Total
		for _, hit := range page.Results {
			if len(hit) != 4 {
				return nil, ErrContentInventoryIncomplete
			}
			var id, documentID string
			var tenant, engine uint
			if json.Unmarshal(hit["id"], &id) != nil || json.Unmarshal(hit["document_id"], &documentID) != nil || json.Unmarshal(hit["tenant_id"], &tenant) != nil || json.Unmarshal(hit["engine_id"], &engine) != nil || id == "" || id != documentID || len(id) > 128 || tenant != tenantID || engine == 0 || seen[id] {
				return nil, ErrContentInventoryIncomplete
			}
			seen[id] = true
			result.Documents = append(result.Documents, ContentIndexIdentity{documentID, tenant, engine})
		}
		offset += int64(len(page.Results))
		if offset > total || offset < total && len(page.Results) == 0 {
			return nil, ErrContentInventoryIncomplete
		}
		if offset == total {
			break
		}
	}
	sort.Slice(result.Documents, func(i, j int) bool { return result.Documents[i].DocumentID < result.Documents[j].DocumentID })
	encoded, err := json.Marshal(struct {
		Index     string                 `json:"index"`
		Tenant    uint                   `json:"tenant_id"`
		Documents []ContentIndexIdentity `json:"documents"`
	}{result.IndexName, result.TenantID, result.Documents})
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	result.Digest = hex.EncodeToString(digest[:])
	return result, nil
}
