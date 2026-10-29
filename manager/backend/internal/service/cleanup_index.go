package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"

	"github.com/addp/common/cleanup"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/engine/selection"
	"github.com/addp/common/events"
	"github.com/addp/manager/internal/models"
	"github.com/meilisearch/meilisearch-go"
	"gorm.io/gorm"
)

// assessCleanupIndex discovers Manager's own projection scope, independently of
// Meta rows. A missing list entry is never evidence that an Engine was deleted.
func (s *CleanupService) assessCleanupIndex(ctx context.Context, input events.CleanupRequest) (*models.CleanupIndexAssessment, *ContentIndexInventory, error) {
	if s.search == nil || s.systemClient == nil {
		return nil, nil, ErrContentInventoryIncomplete
	}
	inventory, err := s.search.ReadContentIndexInventory(ctx, input.TenantID)
	if err != nil {
		return nil, nil, err
	}
	candidates := map[uint]bool{}
	for _, doc := range inventory.Documents {
		if input.Context.EngineID != 0 && input.Context.EngineID != doc.EngineID {
			continue
		}
		candidates[doc.EngineID] = false
	}
	ids := make([]uint, 0, len(candidates))
	for id := range candidates {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	invalid := make([]uint, 0, len(ids))
	for _, id := range ids {
		engine, err := s.systemClient.WithTenantID(input.TenantID).GetEngine(ctx, id)
		if err != nil {
			if code, ok := commonclient.SystemAPIStatusCode(err); !ok || code != http.StatusNotFound {
				return nil, nil, err
			}
			candidates[id] = true
		} else {
			if engine == nil || engine.ID != id || engine.TenantID != nil && *engine.TenantID != input.TenantID {
				return nil, nil, errors.New("cleanup Engine identity mismatch")
			}
			switch engine.LifecycleState {
			case "disabled", "deleting", "deleted":
				candidates[id] = true
			case "active":
				capabilities, err := selection.ParseCapabilities(engine.Capabilities)
				if err != nil || capabilities == nil {
					return nil, nil, fmt.Errorf("cleanup Engine capabilities unavailable")
				}
				candidates[id] = capabilities.Storage == nil
			default:
				return nil, nil, fmt.Errorf("cleanup Engine lifecycle unavailable")
			}
		}
		if candidates[id] {
			invalid = append(invalid, id)
		}
	}
	count := 0
	for _, doc := range inventory.Documents {
		if candidates[doc.EngineID] {
			count++
		}
	}
	plan := &models.CleanupIndexAssessment{RequestID: input.RequestID, TenantID: input.TenantID, RequestedBy: input.RequestedBy, ScopeEngineID: input.Context.EngineID, IndexName: inventory.IndexName, Epoch: inventory.Guard.Epoch, EndpointID: inventory.Guard.EndpointID, InventoryDigest: inventory.Digest, EngineIDs: invalid, CandidateCount: count}
	return plan, inventory, nil
}

// prepareCleanupIndex checks the exact scan scope before taking a durable hold.
// It performs reads only; registration is inside the caller's Run.Effect.
func (s *CleanupService) prepareCleanupIndex(ctx context.Context, run *cleanup.Run) (*models.CleanupIndexAssessment, *ContentIndexInventory, error) {
	var confirmed models.CleanupIndexAssessment
	if err := s.db.WithContext(ctx).Where("request_id=? AND tenant_id=? AND requested_by=?", run.Input.BasedOnScan, run.Input.TenantID, run.Input.RequestedBy).First(&confirmed).Error; err != nil {
		return nil, nil, err
	}
	current, inventory, err := s.assessCleanupIndex(ctx, run.Input)
	if err != nil {
		return nil, nil, err
	}
	if confirmed.ScopeEngineID != current.ScopeEngineID || confirmed.IndexName != current.IndexName || confirmed.Epoch != current.Epoch || confirmed.EndpointID != current.EndpointID || confirmed.InventoryDigest != current.InventoryDigest || confirmed.CandidateCount != current.CandidateCount || !slices.Equal(confirmed.EngineIDs, current.EngineIDs) {
		return nil, nil, fmt.Errorf("cleanup index scope changed")
	}
	return current, inventory, nil
}

// executeCleanupIndex preserves the hold across cancellation, process restart,
// unknown provider response and failed result commit. It never releases the hold;
// finalization shares the private result/receipt/terminal transaction.
func (s *CleanupService) executeCleanupIndex(ctx context.Context, run *cleanup.Run, plan *models.CleanupIndexAssessment, inventory *ContentIndexInventory) (*models.CleanupIndexOperation, error) {
	if len(plan.EngineIDs) == 0 {
		return nil, nil
	}
	var operation *models.CleanupIndexOperation
	if err := run.Effect(ctx, func(tx *gorm.DB) error {
		var err error
		operation, err = s.search.deliveries.BeginCleanupIndex(ctx, tx, run.Lease, inventory.Guard, inventory.Digest, plan.EngineIDs)
		return err
	}); err != nil {
		return nil, err
	}
	held, err := s.search.readContentIndexIdentities(ctx, run.Input.TenantID)
	if err != nil {
		return operation, err
	}
	if held.Digest != inventory.Digest {
		return operation, ErrContentInventoryIncomplete
	}
	for _, engineID := range plan.EngineIDs {
		var delivery *models.ContentIndexDelivery
		effectCtx, cancel, err := run.PrepareExternal(ctx, func(tx *gorm.DB) error {
			var e error
			delivery, e = s.search.deliveries.QueueCleanupDelete(ctx, tx, run.Lease, operation.ID, engineID)
			return e
		})
		if err != nil {
			return operation, err
		}
		task, submitErr := s.search.client.Index(s.search.contentIndex).DeleteDocumentsByFilterWithContext(effectCtx, delivery.Filter, &meilisearch.DocumentOptions{TaskCustomMetadata: delivery.TaskCorrelation})
		err = s.search.recordContentReceipt(effectCtx, delivery, task, submitErr)
		cancel()
		if err != nil {
			return operation, err
		}
		if err := s.search.waitContentDelivery(ctx, delivery); err != nil {
			return operation, err
		}
	}
	return operation, nil
}

func cleanupIndexImpact(plan *models.CleanupIndexAssessment) []events.CleanupImpactItem {
	items := make([]events.CleanupImpactItem, 0, len(plan.EngineIDs))
	for _, id := range plan.EngineIDs {
		items = append(items, events.CleanupImpactItem{StableRef: fmt.Sprintf("manager_index:%s:%d:%s", plan.IndexName, id, plan.InventoryDigest), Disposition: events.CleanupImpactWillDelete})
	}
	return items
}
