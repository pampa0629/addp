package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type glbHousekeepingPlan struct {
	ID, TenantID uint
	Digest       string
	Outputs      []model3DGLBOutputRef
	Inputs       []Model3DGLBInputPreparation
	Tokens       []CleanupTargetToken
}

func glbHousekeepingDigest(item models.Model3DGLB) string {
	b, _ := json.Marshal(item)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func knownGLBWriter(ctx context.Context, db *gorm.DB, tenant uint, id string) bool {
	if id == "" {
		return false
	}
	var op models.CleanupArtifactOperation
	if db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND origin = ? AND state = ?", id, tenant, "generation", "succeeded").First(&op).Error != nil {
		return false
	}
	return op.ExecutionID == id && op.BindingDigest == ArtifactOperationBinding(op)
}

func glbPhysicalTarget(tenant uint, ref string) (CleanupTargetKey, error) {
	bucket, object, err := cog.ObjectLocation(ref, "")
	if err != nil || bucket == "" {
		return CleanupTargetKey{}, commonapi.ErrConflict
	}
	value, _ := json.Marshal([]string{bucket, object})
	return CleanupTargetKey{TenantID: tenant, Kind: CleanupTargetObject, Identity: string(value)}, nil
}

// prepareGLBHousekeeping performs only PG reads/control registration. A failed
// execution is never converted into a stopped writer or a repeated tombstone.
func (r *Model3DGLBRepository) prepareGLBHousekeeping(ctx context.Context, id uint) (*models.CleanupArtifactOperation, error) {
	var operation *models.CleanupArtifactOperation
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
		artifactKey := CleanupTargetKey{TenantID: item.TenantID, Kind: CleanupTargetArtifact, Identity: fmt.Sprintf("%s:%d", execution.TaskTypeModel3DGLBGeneration, item.ID)}
		_, held, err := ReadCleanupTargetSnapshot(ctx, tx, artifactKey)
		if err != nil {
			return err
		}
		if held != "" {
			var existing models.CleanupArtifactOperation
			if err := tx.Where("id = ? AND tenant_id = ? AND origin = ?", held, item.TenantID, "background").First(&existing).Error; err != nil {
				return commonapi.ErrConflict
			}
			if existing.State != "succeeded" || existing.BindingDigest != ArtifactOperationBinding(existing) {
				return commonapi.ErrConflict
			}
			operation = &existing
			return nil
		}
		plan := glbHousekeepingPlan{ID: item.ID, TenantID: item.TenantID, Digest: glbHousekeepingDigest(item)}
		for _, ref := range state.Garbage {
			if ref.StorageRef == item.StorageRef {
				continue
			}
			if ref.Watch && !knownGLBWriter(ctx, tx, item.TenantID, ref.ExecutionID) {
				continue
			}
			var references int64
			if err := tx.Model(&models.Model3DGLB{}).Where("tenant_id = ? AND storage_ref = ?", item.TenantID, ref.StorageRef).Count(&references).Error; err != nil {
				return err
			}
			if references == 0 {
				plan.Outputs = append(plan.Outputs, ref)
			}
		}
		for _, input := range state.InputPreparations {
			if input.Validate(item.TenantID) != nil {
				return commonapi.ErrConflict
			}
			if !input.Cleaned && input.Released && !input.Watch {
				plan.Inputs = append(plan.Inputs, input)
			}
		}
		if len(plan.Outputs)+len(plan.Inputs) == 0 {
			return nil
		}
		keys := []CleanupTargetKey{artifactKey, {TenantID: item.TenantID, Kind: CleanupTargetGeneration, Identity: execution.TaskTypeModel3DGLBGeneration + ":" + item.ItemFingerprint}}
		if item.TaskID != nil {
			keys = append(keys, CleanupTargetKey{TenantID: item.TenantID, Kind: CleanupTargetGeneration, Identity: execution.TaskTypeModel3DGLBGeneration + ":task:" + fmt.Sprint(*item.TaskID)})
		}
		for _, ref := range plan.Outputs {
			key, err := glbPhysicalTarget(item.TenantID, ref.StorageRef)
			if err != nil {
				return err
			}
			keys = append(keys, key)
		}
		for _, input := range plan.Inputs {
			for _, object := range input.Objects {
				value, _ := json.Marshal([]string{object.Bucket, object.Object})
				keys = append(keys, CleanupTargetKey{TenantID: item.TenantID, Kind: CleanupTargetObject, Identity: string(value)})
			}
		}
		seen := map[CleanupTargetKey]bool{}
		for _, key := range keys {
			if seen[key] {
				continue
			}
			seen[key] = true
			token, err := ObserveCleanupTarget(ctx, tx, key)
			if err != nil {
				return err
			}
			plan.Tokens = append(plan.Tokens, token)
		}
		op := models.CleanupArtifactOperation{ID: uuid.NewString(), TenantID: item.TenantID, Origin: "background", Action: "delete", State: "prepared"}
		if err := HoldCleanupTargets(ctx, tx, plan.Tokens, op.ID); err != nil {
			return err
		}
		b, _ := json.Marshal(plan)
		op.Snapshot = string(b)
		b, _ = json.Marshal(plan.Tokens)
		op.TargetKeys = string(b)
		op.BindingDigest = ArtifactOperationBinding(op)
		if err := tx.Create(&op).Error; err != nil {
			return err
		}
		operation = &op
		return nil
	})
	return operation, err
}

func (r *Model3DGLBRepository) runGLBHousekeeping(ctx context.Context, op *models.CleanupArtifactOperation, deleteInput func(context.Context, uint, Model3DGLBInputObject) error, deleteOutput func(context.Context, uint, string) error) (int, error) {
	var plan glbHousekeepingPlan
	if op == nil {
		return 0, nil
	}
	if op.BindingDigest != ArtifactOperationBinding(*op) || json.Unmarshal([]byte(op.Snapshot), &plan) != nil || plan.TenantID != op.TenantID {
		return 0, commonapi.ErrConflict
	}
	count := 0
	if op.State == "prepared" {
		changed := r.db.WithContext(ctx).Model(&models.CleanupArtifactOperation{}).Where("id = ? AND state = ? AND binding_digest = ?", op.ID, "prepared", op.BindingDigest).Update("state", "submitting")
		if changed.Error != nil {
			return 0, changed.Error
		}
		if changed.RowsAffected != 1 {
			return 0, commonapi.ErrConflict
		}
		var effectErr error
		for _, ref := range plan.Outputs {
			if effectErr = deleteOutput(ctx, plan.TenantID, ref.StorageRef); effectErr != nil {
				break
			}
			count++
		}
		if effectErr == nil {
			for _, input := range plan.Inputs {
				for _, object := range input.Objects {
					if deleteInput == nil {
						effectErr = commonapi.ErrConflict
						break
					}
					if effectErr = deleteInput(ctx, plan.TenantID, object); effectErr != nil {
						break
					}
					count++
				}
				if effectErr != nil {
					break
				}
			}
		}
		state := "succeeded"
		if effectErr != nil {
			state = "unknown"
		}
		factCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		changed = r.db.WithContext(factCtx).Model(&models.CleanupArtifactOperation{}).Where("id = ? AND state = ? AND binding_digest = ?", op.ID, "submitting", op.BindingDigest).Updates(map[string]interface{}{"state": state, "effect_at": time.Now().UTC()})
		cancel()
		if changed.Error != nil {
			return count, errors.Join(effectErr, changed.Error)
		}
		if changed.RowsAffected != 1 {
			return count, commonapi.ErrConflict
		}
		if effectErr != nil {
			return count, effectErr
		}
	} else if op.State != "succeeded" {
		return 0, commonapi.ErrConflict
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item models.Model3DGLB
		if err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", plan.ID, plan.TenantID).First(&item).Error; err != nil {
			return err
		}
		if err := hydrateModel3DGLBOutputMetadata(ctx, tx, &item); err != nil {
			return err
		}
		if glbHousekeepingDigest(item) != plan.Digest {
			return commonapi.ErrConflict
		}
		state, err := readModel3DGLBOutputState(item.Metadata)
		if err != nil {
			return err
		}
		deleted := map[string]bool{}
		for _, ref := range plan.Outputs {
			deleted[ref.StorageRef] = true
		}
		kept := state.Garbage[:0]
		for _, ref := range state.Garbage {
			if !deleted[ref.StorageRef] {
				kept = append(kept, ref)
			}
		}
		state.Garbage = kept
		for i, input := range state.InputPreparations {
			for _, done := range plan.Inputs {
				if input.PreparationID == done.PreparationID {
					state.InputPreparations[i].Cleaned = true
				}
			}
		}
		state.CheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := tx.Unscoped().Model(&item).UpdateColumn("metadata", model3DGLBOutputMetadata(item.Metadata, state)).Error; err != nil {
			return err
		}
		if err := FinishCleanupTargets(ctx, tx, plan.Tokens, op.ID, true); err != nil {
			return err
		}
		changed := tx.Model(&models.CleanupArtifactOperation{}).Where("id = ? AND state = ? AND binding_digest = ?", op.ID, "succeeded", op.BindingDigest).Updates(map[string]interface{}{"state": "finalized", "finalized_at": time.Now().UTC()})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return commonapi.ErrConflict
		}
		return nil
	})
	return count, err
}
