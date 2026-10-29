package repository

import (
	"context"
	"fmt"
	"path"
	"reflect"
	"strings"
	"unicode/utf8"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Model3DGLBInputObject is an exact storage identity, never a deletion prefix.
type Model3DGLBInputObject struct {
	Bucket string `json:"bucket"`
	Object string `json:"object"`
}

// Model3DGLBInputPreparation survives failed attempts and result soft deletion.
// Cleaned means the writer is known to have stopped and every planned object
// has been removed. Watch retains an uncertain writer's exact tombstones.
type Model3DGLBInputPreparation struct {
	ExecutionID   string                  `json:"execution_id"`
	Attempt       int                     `json:"attempt"`
	PreparationID string                  `json:"preparation_id"`
	Objects       []Model3DGLBInputObject `json:"objects"`
	Released      bool                    `json:"released"`
	Watch         bool                    `json:"watch"`
	Cleaned       bool                    `json:"cleaned"`
}

func (p Model3DGLBInputPreparation) Validate(tenantID uint) error {
	execution, err := uuid.Parse(p.ExecutionID)
	preparation, prepErr := uuid.Parse(p.PreparationID)
	if tenantID == 0 || err != nil || prepErr != nil || execution == uuid.Nil || preparation == uuid.Nil || execution.String() != p.ExecutionID || preparation.String() != p.PreparationID || p.Attempt <= 0 || len(p.Objects) == 0 || len(p.Objects) > 200 || (p.Cleaned && (!p.Released || p.Watch)) || (p.Watch && !p.Released) {
		return commonAPI.ErrConflict
	}
	prefix := fmt.Sprintf("temp/model3d-input/tenant_%d/%s/attempt_%d/%s/", tenantID, p.ExecutionID, p.Attempt, p.PreparationID)
	seen := make(map[Model3DGLBInputObject]bool, len(p.Objects))
	for _, object := range p.Objects {
		if object.Bucket == "" || object.Bucket != strings.TrimSpace(object.Bucket) || strings.ContainsAny(object.Bucket, "/\\\x00") || !strings.HasPrefix(object.Object, prefix) || len(object.Object) == len(prefix) || !utf8.ValidString(object.Object) || strings.ContainsAny(object.Object, "\\\x00\r\n:") || path.Clean(object.Object) != object.Object || seen[object] {
			return commonAPI.ErrConflict
		}
		seen[object] = true
	}
	return nil
}

// PrepareExecutionInputs commits the complete finite manifest before any PUT.
func (r *Model3DGLBRepository) PrepareExecutionInputs(ctx context.Context, resultID uint, preparation Model3DGLBInputPreparation) error {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || lease.TenantID <= 0 || preparation.ExecutionID != lease.ExecutionID || preparation.Attempt != lease.Attempt || preparation.Released || preparation.Watch || preparation.Cleaned || preparation.Validate(uint(lease.TenantID)) != nil {
		return commonAPI.ErrConflict
	}
	return r.changeExecutionInputs(ctx, resultID, func(state *model3DGLBOutputState) error {
		if state.Pending == nil || state.Pending.ExecutionID != lease.ExecutionID {
			return commonAPI.ErrConflict
		}
		for _, existing := range state.InputPreparations {
			if existing.PreparationID == preparation.PreparationID {
				return commonAPI.ErrConflict
			}
		}
		preparation.Objects = append([]Model3DGLBInputObject(nil), preparation.Objects...)
		state.InputPreparations = append(state.InputPreparations, preparation)
		return nil
	})
}

// FinishExecutionInputs acknowledges only the same live attempt's preparation.
// A stale worker cannot declare a crashed upload settled or erase its evidence.
func (r *Model3DGLBRepository) FinishExecutionInputs(ctx context.Context, resultID uint, preparation Model3DGLBInputPreparation, writesSettled, cleaned bool) error {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || preparation.ExecutionID != lease.ExecutionID || preparation.Attempt != lease.Attempt || lease.TenantID <= 0 || preparation.Validate(uint(lease.TenantID)) != nil {
		return commonAPI.ErrConflict
	}
	return r.changeExecutionInputs(ctx, resultID, func(state *model3DGLBOutputState) error {
		for i := range state.InputPreparations {
			current := &state.InputPreparations[i]
			if current.PreparationID != preparation.PreparationID {
				continue
			}
			if current.ExecutionID != lease.ExecutionID || current.Attempt != lease.Attempt || !reflect.DeepEqual(current.Objects, preparation.Objects) || current.Released {
				return commonAPI.ErrConflict
			}
			current.Released, current.Watch, current.Cleaned = true, !writesSettled, cleaned && writesSettled
			return nil
		}
		return commonAPI.ErrConflict
	})
}

func (r *Model3DGLBRepository) changeExecutionInputs(ctx context.Context, resultID uint, change func(*model3DGLBOutputState) error) error {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || lease.TenantID <= 0 {
		return commonAPI.ErrConflict
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGLBExecutionSource(ctx, tx, lease, 0); err != nil {
			return err
		}
		var item models.Model3DGLB
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", resultID, lease.TenantID).First(&item).Error; err != nil {
			return err
		}
		if !Model3DGLBUsesExecutionOutput(&item) {
			return commonAPI.ErrConflict
		}
		if err := commonExecution.UpdateWithLease(ctx, tx, lease, nil); err != nil {
			return err
		}
		if err := hydrateModel3DGLBOutputMetadata(ctx, tx, &item); err != nil {
			return err
		}
		state, err := readModel3DGLBOutputState(item.Metadata)
		if err != nil {
			return err
		}
		if err := change(&state); err != nil {
			return err
		}
		return tx.Model(&item).UpdateColumn("metadata", model3DGLBOutputMetadata(item.Metadata, state)).Error
	})
}
