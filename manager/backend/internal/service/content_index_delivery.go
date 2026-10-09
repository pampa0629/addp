package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/manager/internal/models"
	managerprotection "github.com/addp/manager/internal/protection"
	"github.com/addp/manager/internal/repository"
	"github.com/meilisearch/meilisearch-go"
	"gorm.io/gorm"
)

func contentIndexEndpointID(endpoint string) string {
	if strings.TrimSpace(endpoint) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.TrimRight(strings.TrimSpace(endpoint), "/")))
	return hex.EncodeToString(sum[:])
}

func applyContentIndexProtection(gate projectionstore.GateReader, tenantID uint, document *commonClient.ManagerContentDocument) error {
	if document == nil {
		return managerprotection.ErrRequired
	}
	if document.PayloadKind != commonClient.ManagerContentPayloadExtractedContent {
		return nil
	}
	now := time.Now().UTC()
	result := managerprotection.DataItemGate(gate, tenantID, document.DocumentID, now)
	return managerprotection.ProtectContentIndexDocument(document, result, now)
}

func (s *HybridSearchService) UpsertContentDocument(ctx context.Context, tenantID uint, document commonClient.ManagerContentDocument) error {
	if !s.Enabled() || !s.initialized.Load() {
		return ErrSearchDisabled
	}
	document.DocumentID = strings.TrimSpace(document.DocumentID)
	if tenantID == 0 || document.Validate() != nil {
		return errors.New("invalid Manager content document")
	}
	version, err := s.protectionStore.CaptureVersion(ctx, int64(tenantID), func(projectionstore.GateReader) error { return nil })
	if err != nil {
		return err
	}
	op := &models.ContentIndexDelivery{TenantID: int64(tenantID), DocumentID: document.DocumentID, Kind: repository.IndexDeliveryWrite}
	var payload map[string]interface{}
	err = s.protectionStore.CommitVersion(ctx, int64(tenantID), version, func(tx *gorm.DB, gate projectionstore.GateReader) error {
		if err := applyContentIndexProtection(gate, tenantID, &document); err != nil {
			return err
		}
		if document.ProjectionTime.IsZero() {
			document.ProjectionTime = time.Now().UTC()
		}
		payload = contentSnapshotPayload(tenantID, document)
		return s.deliveries.Register(ctx, tx, s.epoch, op)
	})
	if err != nil {
		return err
	}
	// No PostgreSQL transaction is held while calling Meilisearch.
	task, err := s.client.Index(s.contentIndex).UpdateDocumentsWithContext(ctx, []map[string]interface{}{payload}, &meilisearch.DocumentOptions{TaskCustomMetadata: op.TaskCorrelation, SkipCreation: true})
	if err := s.recordContentReceipt(ctx, op, task, err); err != nil {
		return err
	}
	return s.waitContentDelivery(ctx, op)
}

func (s *HybridSearchService) recordContentReceipt(ctx context.Context, op *models.ContentIndexDelivery, task *meilisearch.TaskInfo, submitErr error) error {
	// A disconnected caller must not prevent persisting a receipt we received.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	expected := meilisearch.TaskTypeDocumentAdditionOrUpdate
	if op.Kind == repository.IndexDeliveryDelete {
		expected = meilisearch.TaskTypeDocumentDeletion
	}
	if submitErr != nil || task == nil || task.IndexUID != s.contentIndex || task.Type != expected || task.TaskUID < 0 || task.EnqueuedAt.IsZero() {
		_ = s.deliveries.Unknown(persistCtx, op)
		return repository.ErrContentIndexIsolated
	}
	return s.deliveries.Receipt(persistCtx, op, task.TaskUID, task.EnqueuedAt)
}

func (s *HybridSearchService) pollContentDelivery(ctx context.Context, op *models.ContentIndexDelivery) error {
	if op.TaskUID == nil || op.TaskEnqueuedAt == "" || op.EndpointID != s.endpointID {
		return s.fenceContentIndex(ctx)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	task, err := s.client.GetTaskWithContext(ctx, *op.TaskUID)
	if err != nil {
		return s.fenceContentIndex(ctx)
	}
	expected := meilisearch.TaskTypeDocumentAdditionOrUpdate
	if op.Kind == repository.IndexDeliveryDelete {
		expected = meilisearch.TaskTypeDocumentDeletion
	}
	if task == nil || task.UID != *op.TaskUID || task.IndexUID != s.contentIndex || task.Type != expected || task.EnqueuedAt.UTC().Format(time.RFC3339Nano) != op.TaskEnqueuedAt || (op.TaskCorrelation != "" && task.CustomMetadata != op.TaskCorrelation) {
		return s.fenceContentIndex(ctx)
	}
	switch task.Status {
	case meilisearch.TaskStatusSucceeded, meilisearch.TaskStatusFailed, meilisearch.TaskStatusCanceled:
		return s.deliveries.Finish(ctx, op, string(task.Status))
	case meilisearch.TaskStatusEnqueued, meilisearch.TaskStatusProcessing:
		return nil
	default:
		return s.fenceContentIndex(ctx)
	}
}

func (s *HybridSearchService) fenceContentIndex(ctx context.Context) error {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.deliveries.Fence(persistCtx, s.epoch); err != nil {
		return err
	}
	return repository.ErrContentIndexIsolated
}

func (s *HybridSearchService) processContentDelivery(ctx context.Context, op *models.ContentIndexDelivery) error {
	if op.Status == repository.IndexDeliveryQueued {
		claimed, err := s.deliveries.ClaimDelete(ctx, s.epoch, op)
		if err != nil || !claimed {
			return err
		}
		task, err := s.client.Index(s.contentIndex).DeleteDocumentsByFilterWithContext(ctx, op.Filter, &meilisearch.DocumentOptions{TaskCustomMetadata: op.TaskCorrelation})
		if err := s.recordContentReceipt(ctx, op, task, err); err != nil {
			return err
		}
	}
	if op.Status == repository.IndexDeliverySubmitting || op.Status == repository.IndexDeliveryUnknown {
		if err := s.recoverContentReceipt(ctx, op); err != nil {
			if errors.Is(err, repository.ErrContentIndexIsolated) {
				return s.fenceContentIndex(ctx)
			}
			return err
		}
	}
	if op.Status == repository.IndexDeliverySubmitted {
		return s.pollContentDelivery(ctx, op)
	}
	return nil
}

func (s *HybridSearchService) waitContentDelivery(ctx context.Context, original *models.ContentIndexDelivery) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		op, err := s.deliveries.Get(ctx, original.TenantID, original.ID)
		if err != nil {
			return err
		}
		switch op.Status {
		case repository.IndexDeliverySucceeded:
			// Other pending deliveries may legitimately keep the outlet isolated.
			if err := s.deliveries.ActivateIfSettled(ctx, s.epoch); err != nil && !errors.Is(err, repository.ErrContentIndexIsolated) {
				return err
			}
			return nil
		case repository.IndexDeliveryFailed, repository.IndexDeliveryCanceled, repository.IndexDeliveryUnknown:
			return repository.ErrContentIndexIsolated
		}
		if err := s.processContentDelivery(ctx, op); err != nil && !errors.Is(err, repository.ErrContentIndexDeliveryConflict) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// StartContentRecovery continues receipts, correlated submissions and purges.
// It never resends a write, or takes over an uncertain submission by timeout.
func (s *HybridSearchService) StartContentRecovery(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			cycleCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			err := s.reconcileContentDeliveries(cycleCtx)
			cancel()
			if err != nil && !errors.Is(err, context.Canceled) {
				s.log.Warn("索引投递尚未收敛，保留未决记录")
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *HybridSearchService) reconcileContentDeliveries(ctx context.Context) error {
	if _, err := s.deliveries.State(ctx, s.epoch); err != nil {
		return err
	}
	if !s.initialized.Load() {
		if err := s.initIndexes(ctx); err != nil {
			return ErrSearchDisabled
		}
		s.initialized.Store(true)
	}
	ops, err := s.deliveries.Recoverable(ctx)
	if err != nil {
		return err
	}
	for _, op := range ops {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.processContentDelivery(ctx, &op); err != nil && !errors.Is(err, repository.ErrContentIndexDeliveryConflict) && !errors.Is(err, repository.ErrContentIndexIsolated) {
			return err
		}
	}
	return s.deliveries.ActivateIfSettled(ctx, s.epoch)
}

func supportsContentTaskCorrelation(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil || major != 1 {
		return false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil || minor < 26 {
		return false
	}
	patch, err := strconv.Atoi(parts[2])
	return err == nil && patch >= 0
}

// recoverContentReceipt only reads retained history; absence is never a resend
// decision. Scan all task types/indexes to detect conflicting uses of a marker.
func (s *HybridSearchService) recoverContentReceipt(ctx context.Context, op *models.ContentIndexDelivery) error {
	if op == nil || op.TaskCorrelation == "" || op.TaskCorrelation != op.ID || op.EndpointID != s.endpointID || op.IndexName != s.contentIndex {
		return repository.ErrContentIndexIsolated
	}
	const budget = int64(10000)
	query := &meilisearch.TasksQuery{Limit: 100}
	var match *meilisearch.Task
	var total, seen int64
	last := int64(-1)
	expected := meilisearch.TaskTypeDocumentAdditionOrUpdate
	if op.Kind == repository.IndexDeliveryDelete {
		expected = meilisearch.TaskTypeDocumentDeletion
	}
	check := func(task meilisearch.Task) error {
		if task.UID < 0 || (seen > 0 && task.UID >= last) {
			return repository.ErrContentIndexIsolated
		}
		seen++
		last = task.UID
		if task.CustomMetadata != op.TaskCorrelation {
			return nil
		}
		if match != nil || task.IndexUID != s.contentIndex || task.Type != expected || task.EnqueuedAt.IsZero() {
			return repository.ErrContentIndexIsolated
		}
		copy := task
		match = &copy
		return nil
	}
	for {
		page, err := s.client.GetTasksWithContext(ctx, query)
		if err != nil || page == nil || page.Total < 0 || page.Total > budget || len(page.Results) > 100 {
			return repository.ErrContentIndexIsolated
		}
		if seen == 0 {
			total = page.Total
		} else if page.Total != total {
			return repository.ErrContentIndexIsolated
		}
		for _, task := range page.Results {
			if err := check(task); err != nil {
				return err
			}
		}
		if seen == total {
			if page.Next != 0 {
				return repository.ErrContentIndexIsolated
			}
			break
		}
		if seen > total || len(page.Results) == 0 || page.Next < 0 || page.Next >= last {
			return repository.ErrContentIndexIsolated
		}
		// The SDK omits from=0, and represents JSON null as 0. When the
		// retained total proves one remaining task, read UID 0 explicitly.
		if page.Next == 0 {
			if seen+1 != total {
				return repository.ErrContentIndexIsolated
			}
			task, err := s.client.GetTaskWithContext(ctx, 0)
			if err != nil || task == nil || task.UID != 0 {
				return repository.ErrContentIndexIsolated
			}
			if err := check(*task); err != nil {
				return err
			}
			break
		}
		query.From = page.Next
	}
	if match == nil {
		return repository.ErrContentIndexIsolated
	}
	return s.deliveries.Receipt(ctx, op, match.UID, match.EnqueuedAt)
}

// ReadyToAcknowledge proves the outlet is fenced or fully settled; it does not
// claim that isolated external documents were removed. HTTP drain is separate.
func (s *HybridSearchService) ReadyToAcknowledge(ctx context.Context, tenantID int64, cursor string) error {
	if s == nil || tenantID <= 0 || cursor == "" {
		return repository.ErrContentIndexIsolated
	}
	state, err := s.deliveries.State(ctx, s.epoch)
	if err != nil {
		return err
	}
	if state.Isolated {
		return nil
	}
	if err := s.deliveries.RequireActive(ctx, s.epoch); err != nil {
		if !errors.Is(err, repository.ErrContentIndexIsolated) {
			return err
		}
		return s.deliveries.Fence(ctx, s.epoch)
	}
	return nil
}
