package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const model3DGLBOutputStateKey = "model3d_output_lifecycle"
const model3DGLBOutputOwnerKey = "model3d_output_owner"

func Model3DGLBUsesExecutionOutput(item *models.Model3DGLB) bool {
	return item != nil && item.Metadata[model3DGLBOutputOwnerKey] == "manager_execution"
}

type model3DGLBOutputRef struct {
	ExecutionID string `json:"execution_id"`
	TaskID      string `json:"task_id,omitempty"`
	StorageRef  string `json:"storage_ref"`
	// An interrupted remote invocation may write after its caller has failed.
	// Keep its exact tombstone for repeated cleanup rather than infer termination
	// from a successful DELETE of an object that might not exist yet.
	Watch bool `json:"watch,omitempty"`
}

type model3DGLBOutputState struct {
	Pending             *model3DGLBOutputRef         `json:"pending,omitempty"`
	Garbage             []model3DGLBOutputRef        `json:"garbage,omitempty"`
	InputPreparations   []Model3DGLBInputPreparation `json:"input_preparations,omitempty"`
	InputCleanupPending bool                         `json:"input_cleanup_pending,omitempty"`
	CheckedAt           string                       `json:"checked_at,omitempty"`
}

func readModel3DGLBOutputState(metadata commonModels.JSONMap) (model3DGLBOutputState, error) {
	var state model3DGLBOutputState
	value, ok := metadata[model3DGLBOutputStateKey]
	if !ok {
		return state, nil
	}
	raw, err := json.Marshal(value)
	if err != nil || json.Unmarshal(raw, &state) != nil {
		return state, commonAPI.ErrConflict
	}
	if state.Pending != nil {
		id, err := strconv.ParseInt(state.Pending.TaskID, 10, 64)
		if state.Pending.ExecutionID == "" || state.Pending.StorageRef == "" || err != nil || id <= 0 || strconv.FormatInt(id, 10) != state.Pending.TaskID {
			return state, commonAPI.ErrConflict
		}
	}
	for _, ref := range state.Garbage {
		if ref.StorageRef == "" {
			return state, commonAPI.ErrConflict
		}
	}
	return state, nil
}

func model3DGLBOutputMetadata(metadata commonModels.JSONMap, state model3DGLBOutputState) commonModels.JSONMap {
	// Only the private root keys change. A JSONMap.Clone round trip would
	// silently turn exact scope identifiers into float64 values.
	result := make(commonModels.JSONMap, len(metadata)+2)
	for key, value := range metadata {
		result[key] = value
	}
	result[model3DGLBOutputOwnerKey] = "manager_execution"
	// Maintain the candidate marker in this sole lifecycle writer. Retained
	// cleaned manifests are audit facts, not background cleanup work.
	state.InputCleanupPending = false
	for _, input := range state.InputPreparations {
		if !input.Cleaned {
			state.InputCleanupPending = true
			break
		}
	}
	if state.Pending == nil && len(state.Garbage) == 0 && len(state.InputPreparations) == 0 {
		delete(result, model3DGLBOutputStateKey)
	} else {
		result[model3DGLBOutputStateKey] = state
	}
	return result
}

// All callers hold this result row's lock, so the raw JSON and visible artifact
// facts belong to the same row version. Preserve exact source-scope integers.
func hydrateModel3DGLBOutputMetadata(ctx context.Context, tx *gorm.DB, item *models.Model3DGLB) error {
	var raw []byte
	if err := tx.WithContext(ctx).Unscoped().Model(&models.Model3DGLB{}).Select("metadata").Where("id = ? AND tenant_id = ?", item.ID, item.TenantID).Row().Scan(&raw); err != nil {
		return err
	}
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		item.Metadata = nil
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&item.Metadata); err != nil {
		return err
	}
	return nil
}

// DeleteExecutionOutputResult serializes with pending creation on the task row
// before touching the published object. The retained tombstones remain visible
// to background cleanup after this result is soft-deleted.
func (r *Model3DGLBRepository) DeleteExecutionOutputResult(ctx context.Context, expected *models.Model3DGLB, deleteExact func(context.Context, uint, string) error) error {
	if !Model3DGLBUsesExecutionOutput(expected) || expected.TaskID == nil || deleteExact == nil {
		return commonAPI.ErrConflict
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task models.Model3DGLBTask
		if err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ? AND task_type = ?", *expected.TaskID, expected.TenantID, commonExecution.TaskTypeModel3DGLBGeneration).First(&task).Error; err != nil {
			return err
		}
		active, err := activeModel3DGLBSourceExecutions(tx, expected.TenantID, expected.ItemFingerprint)
		if err != nil {
			return err
		}
		if active != 0 {
			return commonAPI.ErrConflict
		}
		var current models.Model3DGLB
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", expected.ID, expected.TenantID).First(&current).Error; err != nil {
			return err
		}
		if !Model3DGLBUsesExecutionOutput(&current) || current.TaskID == nil || *current.TaskID != task.ID {
			return commonAPI.ErrConflict
		}
		if err := hydrateModel3DGLBOutputMetadata(ctx, tx, &current); err != nil {
			return err
		}
		state, err := readModel3DGLBOutputState(current.Metadata)
		if err != nil || state.Pending != nil {
			return commonAPI.ErrConflict
		}
		active, err = activeModel3DGLBSourceExecutions(tx, current.TenantID, current.ItemFingerprint)
		if err != nil {
			return err
		}
		if active != 0 {
			return commonAPI.ErrConflict
		}
		if current.StorageRef != "" {
			var references int64
			if err := tx.Model(&models.Model3DGLB{}).Where("tenant_id = ? AND storage_ref = ? AND id <> ?", current.TenantID, current.StorageRef, current.ID).Count(&references).Error; err != nil {
				return err
			}
			if references == 0 {
				if err := deleteExact(ctx, current.TenantID, current.StorageRef); err != nil {
					return err
				}
			}
		}
		return NewModel3DGLBRepository(tx).Delete(ctx, current.ID, current.TenantID)
	})
}

func activeModel3DGLBSourceExecutions(tx *gorm.DB, tenantID uint, fingerprint string) (int64, error) {
	if fingerprint == "" {
		return 0, commonAPI.ErrConflict
	}
	var count int64
	err := tx.Table("common.task_executions AS executions").Joins("JOIN manager.task_definitions AS definitions ON definitions.tenant_id = executions.tenant_id AND CAST(definitions.id AS TEXT) = executions.source_task_id").Where("executions.tenant_id = ? AND executions.module = ? AND executions.task_type = ? AND executions.status IN ? AND definitions.task_type = ? AND definitions.semantic_key = ?", tenantID, commonExecution.ModuleManager, commonExecution.TaskTypeModel3DGLBGeneration, []string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning}, commonExecution.TaskTypeModel3DGLBGeneration, fingerprint).Count(&count).Error
	return count, err
}

func appendModel3DGLBGarbage(state *model3DGLBOutputState, ref model3DGLBOutputRef) {
	for i, old := range state.Garbage {
		if old.StorageRef == ref.StorageRef {
			state.Garbage[i].Watch = old.Watch || ref.Watch
			return
		}
	}
	state.Garbage = append(state.Garbage, ref)
}

// PrepareExecutionOutput records the new object's ownership before any remote
// write, while retaining the currently published storage_ref. No source grant
// or output visibility is created by this preparation.
func (r *Model3DGLBRepository) PrepareExecutionOutput(ctx context.Context, desired *models.Model3DGLB, outputRef string) (*models.Model3DGLB, error) {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || desired == nil || desired.TenantID == 0 || desired.TaskID == nil || *desired.TaskID == 0 || desired.ItemFingerprint == "" || desired.SourceEngineID == 0 ||
		!commonExecution.ManagerModel3DConversionFormat(desired.SourceFormat) || desired.LastExecutionID == nil || *desired.LastExecutionID != lease.ExecutionID || int(desired.TenantID) != lease.TenantID || outputRef == "" {
		return nil, commonAPI.ErrConflict
	}
	var result models.Model3DGLB
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ownerCount int64
		if err := tx.Model(&commonExecution.TaskExecution{}).Where("tenant_id = ? AND execution_id = ? AND module = ? AND task_type = ? AND source_task_id = ?", desired.TenantID, lease.ExecutionID, commonExecution.ModuleManager, commonExecution.TaskTypeModel3DGLBGeneration, strconv.FormatUint(uint64(*desired.TaskID), 10)).Count(&ownerCount).Error; err != nil {
			return err
		}
		if ownerCount != 1 {
			return commonAPI.ErrConflict
		}
		if err := commonExecution.UpdateWithLease(ctx, tx, lease, map[string]interface{}{"updated_at": time.Now().UTC()}); err != nil {
			return err
		}
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND item_fingerprint = ? AND status <> ?", desired.TenantID, desired.ItemFingerprint, models.Model3DGLBStatusDeleted).Order("updated_at DESC, id DESC").First(&result).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			result = *desired
			result.ID = 0
			result.StorageRef = ""
			result.Status = models.Model3DGLBStatusBuilding
			result.Metadata = model3DGLBOutputMetadata(nil, model3DGLBOutputState{Pending: &model3DGLBOutputRef{ExecutionID: lease.ExecutionID, TaskID: strconv.FormatUint(uint64(*desired.TaskID), 10), StorageRef: outputRef}})
			return tx.Create(&result).Error
		}
		if err != nil {
			return err
		}
		if err := hydrateModel3DGLBOutputMetadata(ctx, tx, &result); err != nil {
			return err
		}
		state, err := readModel3DGLBOutputState(result.Metadata)
		if err != nil || state.Pending != nil {
			return commonAPI.ErrConflict
		}
		if result.StorageRef == outputRef {
			return commonAPI.ErrConflict
		}
		state.Pending = &model3DGLBOutputRef{ExecutionID: lease.ExecutionID, TaskID: strconv.FormatUint(uint64(*desired.TaskID), 10), StorageRef: outputRef}
		// A previously published artifact remains visible with its original
		// version until a successful atomic switch. The pending marker carries
		// ownership without changing the visible last_execution_id.
		fields := map[string]interface{}{"metadata": model3DGLBOutputMetadata(result.Metadata, state), "updated_at": time.Now().UTC()}
		if err := tx.Model(&result).Updates(fields).Error; err != nil {
			return err
		}
		return tx.First(&result, result.ID).Error
	})
	return &result, err
}

// CompleteExecutionOutput switches the object pointer and all existing owner
// terminal facts in one transaction. Garbage ownership is retained in that same
// record, so a crash after commit does not lose the old object to be reclaimed.
func (r *Model3DGLBRepository) CompleteExecutionOutput(ctx context.Context, taskID, tenantID uint, executionID string, resultID uint, outputRef string, resultFields, executionFields map[string]interface{}, completedAt time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.Model3DGLB
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", resultID, tenantID).First(&current).Error; err != nil {
			return err
		}
		if err := hydrateModel3DGLBOutputMetadata(ctx, tx, &current); err != nil {
			return err
		}
		state, err := readModel3DGLBOutputState(current.Metadata)
		if err != nil || state.Pending == nil || state.Pending.TaskID != strconv.FormatUint(uint64(taskID), 10) || state.Pending.ExecutionID != executionID || state.Pending.StorageRef != outputRef {
			return commonAPI.ErrConflict
		}
		fields := map[string]interface{}{}
		for k, v := range resultFields {
			fields[k] = v
		}
		metadata := current.Metadata
		if executionFields["status"] == commonExecution.ExecutionStatusSuccess {
			if fields["storage_ref"] != outputRef || fields["status"] != models.Model3DGLBStatusReady {
				return commonAPI.ErrConflict
			}
			fields["last_execution_id"] = executionID
			fields["task_id"] = taskID
			if current.StorageRef != "" && current.StorageRef != outputRef {
				appendModel3DGLBGarbage(&state, model3DGLBOutputRef{StorageRef: current.StorageRef})
			}
			if built, ok := fields["metadata"].(commonModels.JSONMap); ok {
				metadata = built
			}
		} else {
			// Preserve every published fact on failure, including its renderer
			// version. Only a first generation with no current object becomes failed.
			fields = map[string]interface{}{}
			if current.StorageRef == "" {
				fields["status"] = models.Model3DGLBStatusFailed
				fields["error_message"] = resultFields["error_message"]
			}
			appendModel3DGLBGarbage(&state, model3DGLBOutputRef{ExecutionID: executionID, StorageRef: outputRef, Watch: true})
		}
		state.Pending = nil
		fields["metadata"] = model3DGLBOutputMetadata(metadata, state)
		if err := tx.Model(&current).Updates(fields).Error; err != nil {
			return err
		}
		// The result update above and the standard owner/execution terminal
		// transaction below share the same outer transaction and lease fence.
		return newTaskExecutionLifecycle(tx).Complete(ctx, taskID, tenantID, executionID, completedAt, taskExecutionCompletionSpec{TaskModel: &models.Model3DGLBTask{}, ExecutionFields: executionFields}, "model 3d GLB")
	})
}

// CleanupExecutionOutputs also sees soft-deleted result records. It never lists
// storage directories, and never deletes the current pointer or a live pending
// execution's output. deleteExact must validate Manager bucket/tenant ownership.
func (r *Model3DGLBRepository) CleanupExecutionOutputs(ctx context.Context, limit int, deleteInput func(context.Context, uint, Model3DGLBInputObject) error, deleteExact func(context.Context, uint, string) error) (int, error) {
	if deleteExact == nil || limit <= 0 {
		return 0, commonAPI.ErrConflict
	}
	var ids []uint
	if err := r.db.WithContext(ctx).Unscoped().Model(&models.Model3DGLB{}).Where("(metadata -> ? -> 'pending') IS NOT NULL OR (metadata -> ? -> 'garbage') IS NOT NULL OR (metadata -> ? -> 'input_cleanup_pending') IS NOT NULL", model3DGLBOutputStateKey, model3DGLBOutputStateKey, model3DGLBOutputStateKey).Order("COALESCE(metadata->'"+model3DGLBOutputStateKey+"'->>'checked_at',''), id").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	deleted := 0
	var cleanupErr error
	for _, id := range ids {
		var objectErr error
		err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var item models.Model3DGLB
			if err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, id).Error; err != nil {
				return err
			}
			if err := hydrateModel3DGLBOutputMetadata(ctx, tx, &item); err != nil {
				return err
			}
			state, err := readModel3DGLBOutputState(item.Metadata)
			if err != nil {
				return err
			}
			if state.Pending != nil {
				var execution commonExecution.TaskExecution
				err := tx.Where("tenant_id = ? AND execution_id = ? AND module = ? AND task_type = ?", item.TenantID, state.Pending.ExecutionID, commonExecution.ModuleManager, commonExecution.TaskTypeModel3DGLBGeneration).First(&execution).Error
				if err != nil {
					return err
				} // Missing evidence never means the invocation stopped.
				switch execution.Status {
				case commonExecution.ExecutionStatusFailed, commonExecution.ExecutionStatusTimeout, commonExecution.ExecutionStatusCancelled:
					ref := *state.Pending
					ref.Watch = true
					appendModel3DGLBGarbage(&state, ref)
					state.Pending = nil
				}
			}
			remaining := make([]model3DGLBOutputRef, 0, len(state.Garbage))
			for _, ref := range state.Garbage {
				if ref.StorageRef == item.StorageRef {
					objectErr = errors.Join(objectErr, commonAPI.ErrConflict)
					remaining = append(remaining, ref)
					continue
				}
				var references int64
				if err := tx.Model(&models.Model3DGLB{}).Where("tenant_id = ? AND storage_ref = ?", item.TenantID, ref.StorageRef).Count(&references).Error; err != nil {
					return err
				}
				if references != 0 {
					remaining = append(remaining, ref)
					continue
				}
				if err := deleteExact(ctx, item.TenantID, ref.StorageRef); err != nil {
					objectErr = errors.Join(objectErr, err)
					remaining = append(remaining, ref)
					continue
				}
				deleted++
				if ref.Watch {
					remaining = append(remaining, ref)
				}
			}
			state.Garbage = remaining
			inputCount, inputErr := cleanupModel3DGLBInputs(ctx, tx, &item, &state, deleteInput)
			deleted += inputCount
			objectErr = errors.Join(objectErr, inputErr)
			state.CheckedAt = time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
			// Housekeeping cannot promote a historical artifact in the current
			// result selector, whose ordering is updated_at then id.
			return tx.Unscoped().Model(&item).UpdateColumn("metadata", model3DGLBOutputMetadata(item.Metadata, state)).Error
		})
		if err != nil || objectErr != nil {
			cleanupErr = errors.Join(cleanupErr, err, objectErr)
		}
	}
	return deleted, cleanupErr
}
