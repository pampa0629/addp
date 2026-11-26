package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
	ms "github.com/meilisearch/meilisearch-go"
	"gorm.io/gorm"
)

// MaintainSearch runs only from the offline deployment command. The API has no
// endpoint for full-index maintenance. Repeating its operation ID resumes it.
func MaintainSearch(ctx context.Context, db *gorm.DB, cfg *config.Config, action, id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id || parsed == uuid.Nil || (action != "clear" && action != "rebuild") {
		return errors.New("expected clear or rebuild and a canonical operation UUID")
	}
	if cfg.MeilisearchURL == "" || cfg.MeilisearchManagerContentIndex == "" {
		return ErrSearchDisabled
	}
	index := cfg.MeilisearchManagerContentIndex
	repo := repository.NewContentIndexDeliveryRepository(db, index)
	client := ms.New(cfg.MeilisearchURL, ms.WithAPIKey(cfg.MeilisearchMasterKey), ms.WithCustomClient(&http.Client{Timeout: 15 * time.Second}), ms.DisableRetries())
	endpoint := config.SearchEndpointID(cfg.MeilisearchURL)
	for {
		op, err := repo.BeginSearchMaintenance(ctx, id, endpoint, action)
		if err != nil {
			return err
		}
		if op.Phase == "done" {
			return nil
		}
		target := client.Index(index)
		switch op.Phase {
		case "prepare":
			info, err := client.GetIndexWithContext(ctx, index)
			if err != nil {
				return err
			}
			if info.UID != index || info.PrimaryKey != "id" {
				return errors.New("unexpected Manager index identity")
			}
			stats, err := target.GetStatsWithContext(ctx, nil)
			if err != nil {
				return err
			}
			fields, err := target.GetSearchableAttributesWithContext(ctx)
			if err != nil {
				return err
			}
			if fields == nil || len(*fields) == 0 || slices.Equal(*fields, []string{maintenanceField(id)}) {
				return errors.New("unexpected Manager searchable attributes")
			}
			encoded, err := json.Marshal(*fields)
			if err != nil {
				return err
			}
			err = repo.AdvanceSearchMaintenance(ctx, op, map[string]any{"phase": "ready", "document_count": stats.NumberOfDocuments, "searchable": string(encoded)})
			if err != nil {
				return err
			}
		case "ready":
			running, err := client.GetTasksWithContext(ctx, &ms.TasksQuery{IndexUIDS: []string{index}, Statuses: []ms.TaskStatus{ms.TaskStatusProcessing, ms.TaskStatusEnqueued}, Limit: 1})
			if err != nil {
				return err
			}
			if running.Total != 0 {
				return errors.New("Manager index has unfinished external tasks")
			}
			recent, err := client.GetTasksWithContext(ctx, &ms.TasksQuery{IndexUIDS: []string{index}, Limit: 1})
			if err != nil {
				return err
			}
			prev := int64(-1)
			if len(recent.Results) > 0 {
				prev = recent.Results[0].UID
			}
			if err := repo.AdvanceSearchMaintenance(ctx, op, map[string]any{"phase": "dispatching", "previous_uid": prev}); err != nil {
				return err
			}
			// After this commit an uncertain submission is only reconciled, never resent.
			var task *ms.TaskInfo
			if action == "clear" {
				task, err = target.DeleteAllDocumentsWithContext(ctx, &ms.DocumentOptions{TaskCustomMetadata: id})
			} else {
				fields, fieldErr := maintenanceFields(op)
				if fieldErr != nil {
					return fieldErr
				}
				task, err = target.UpdateSearchableAttributesWithContext(ctx, &fields)
			}
			if err != nil {
				return fmt.Errorf("index maintenance submission has an unknown outcome: %w", err)
			}
			if task == nil || task.IndexUID != index || task.TaskUID <= prev || task.EnqueuedAt.IsZero() || task.Type != maintenanceTaskType(action) {
				return errors.New("unexpected index maintenance receipt")
			}
			op.Phase = "dispatching"
			if err := recordMaintenanceTask(ctx, repo, op, task.TaskUID, task.EnqueuedAt); err != nil {
				return err
			}
		case "dispatching":
			// Only the frozen Manager index is in scope. There must be exactly
			// one matching operation since the recorded high-water mark.
			recent, err := client.GetTasksWithContext(ctx, &ms.TasksQuery{IndexUIDS: []string{index}, Limit: 2})
			if err != nil {
				return err
			}
			if len(recent.Results) == 0 || recent.Results[0].UID <= op.PreviousUID || len(recent.Results) > 1 && recent.Results[1].UID > op.PreviousUID {
				return repository.ErrContentIndexIsolated
			}
			task := recent.Results[0]
			if !maintenanceTaskMatches(op, &task) {
				return repository.ErrContentIndexIsolated
			}
			if err := recordMaintenanceTask(ctx, repo, op, task.UID, task.EnqueuedAt); err != nil {
				return err
			}
		case "submitted":
			if op.TaskUID == nil {
				return errors.New("missing index maintenance task UID")
			}
			task, err := client.WaitForTaskWithContext(ctx, *op.TaskUID, 200*time.Millisecond)
			if err != nil {
				return err
			}
			if !maintenanceTaskMatches(op, task) || task.UID != *op.TaskUID || task.EnqueuedAt.UTC().Format(time.RFC3339Nano) != op.TaskEnqueuedAt || task.Status != ms.TaskStatusSucceeded {
				return fmt.Errorf("index maintenance task %d did not confirm the requested effect", *op.TaskUID)
			}
			phase := "verify"
			if action == "rebuild" && op.Step == 0 {
				phase = "ready"
			}
			if err := repo.AdvanceSearchMaintenance(ctx, op, map[string]any{"phase": phase, "step": op.Step + 1}); err != nil {
				return err
			}
		case "verify":
			expected := op.DocumentCount
			if action == "clear" {
				expected = 0
			}
			if err := verifyMaintenanceDocuments(ctx, target, repo, expected); err != nil {
				return err
			}
			fields, err := target.GetSearchableAttributesWithContext(ctx)
			if err != nil {
				return err
			}
			var original []string
			if err := json.Unmarshal([]byte(op.Searchable), &original); err != nil {
				return err
			}
			if fields == nil || !slices.Equal(*fields, original) {
				return errors.New("Manager search settings differ after maintenance")
			}
			if err := repo.AdvanceSearchMaintenance(ctx, op, map[string]any{"phase": "done"}); err != nil {
				return err
			}
		default:
			return fmt.Errorf("invalid Manager search maintenance phase %q", op.Phase)
		}
	}
}

func maintenanceField(id string) string { return "__addp_rebuild_" + id }
func maintenanceFields(op *repository.SearchMaintenance) ([]string, error) {
	if op.Step == 0 {
		return []string{maintenanceField(op.ID)}, nil
	}
	var fields []string
	err := json.Unmarshal([]byte(op.Searchable), &fields)
	return fields, err
}
func maintenanceTaskType(action string) ms.TaskType {
	if action == "clear" {
		return ms.TaskTypeDocumentDeletion
	}
	return ms.TaskTypeSettingsUpdate
}
func maintenanceTaskMatches(op *repository.SearchMaintenance, t *ms.Task) bool {
	if t == nil || t.IndexUID != op.IndexName || t.Type != maintenanceTaskType(op.Action) || t.UID <= op.PreviousUID || t.EnqueuedAt.IsZero() {
		return false
	}
	if op.Action == "clear" {
		return t.CustomMetadata == op.ID
	}
	fields, err := maintenanceFields(op)
	return err == nil && slices.Equal(t.Details.SearchableAttributes, fields)
}
func recordMaintenanceTask(ctx context.Context, repo *repository.ContentIndexDeliveryRepository, op *repository.SearchMaintenance, uid int64, at time.Time) error {
	return repo.AdvanceSearchMaintenance(ctx, op, map[string]any{"phase": "submitted", "task_uid": uid, "task_enqueued_at": at.UTC().Format(time.RFC3339Nano)})
}

func verifyMaintenanceDocuments(ctx context.Context, index ms.IndexManager, repo *repository.ContentIndexDeliveryRepository, expected int64) error {
	for offset := int64(0); ; {
		var page ms.DocumentsResult
		if err := index.GetDocumentsWithContext(ctx, &ms.DocumentsQuery{Limit: 100, Offset: offset, Fields: []string{"id", "document_id", "search_ordinal"}}, &page); err != nil {
			return err
		}
		var docs []struct {
			ID         string `json:"id"`
			DocumentID string `json:"document_id"`
			Ordinal    int64  `json:"search_ordinal"`
		}
		if err := page.Results.DecodeInto(&docs); err != nil {
			return err
		}
		wanted := expected - offset
		if wanted > 100 {
			wanted = 100
		}
		if page.Total != expected || page.Offset != offset || int64(len(docs)) != wanted {
			return errors.New("Manager document count changed during index maintenance")
		}
		if len(docs) == 0 {
			return nil
		}
		assigned := make(map[string]int64, len(docs))
		for _, doc := range docs {
			if doc.ID == "" || doc.ID != doc.DocumentID || doc.Ordinal <= 0 {
				return errors.New("invalid rebuilt Manager document identity")
			}
			assigned[doc.ID] = doc.Ordinal
		}
		if len(assigned) != len(docs) {
			return errors.New("duplicate Manager document identity")
		}
		if err := repo.CheckSearchOrdinals(ctx, assigned); err != nil {
			return err
		}
		offset += int64(len(docs))
		if offset == expected {
			return nil
		}
	}
}
