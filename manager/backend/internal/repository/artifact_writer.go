package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

type artifactWriterContextKey struct{}
type artifactWriterTicket struct {
	mu        sync.Mutex
	operation *models.CleanupArtifactOperation
}

func ArtifactWriterContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, artifactWriterContextKey{}, &artifactWriterTicket{})
}

func ArtifactOperationBinding(op models.CleanupArtifactOperation) string {
	b, _ := json.Marshal([]interface{}{op.ID, op.TenantID, op.Origin, op.ExecutionID, op.Attempt, op.LeaseToken, op.LeaseOwner, op.Action, op.Snapshot, op.TargetKeys})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// BeginArtifactWrite is called before the first external mutation, including
// an overwrite's deletion. Only this in-memory invocation may continue its own
// submitting operation; another process/invocation cannot replay that request.
func BeginArtifactWrite(ctx context.Context, db *gorm.DB) error {
	ticket, ok := ctx.Value(artifactWriterContextKey{}).(*artifactWriterTicket)
	if !ok {
		return commonapi.ErrConflict
	}
	lease, ok := execution.LeaseFromContext(ctx)
	if !ok {
		return commonapi.ErrConflict
	}
	ticket.mu.Lock()
	defer ticket.mu.Unlock()

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := execution.LockActiveLease(ctx, tx, lease)
		if err != nil {
			return err
		}
		if item.Module != execution.ModuleManager || !artifactGenerationType(item.TaskType) {
			return commonapi.ErrConflict
		}
		if ticket.operation != nil {
			op := ticket.operation
			if op.ExecutionID != lease.ExecutionID || op.Attempt != lease.Attempt || op.LeaseToken != lease.Token || op.LeaseOwner != lease.Owner {
				return commonapi.ErrConflict
			}
			var current models.CleanupArtifactOperation
			if err := tx.Where("id = ? AND origin = ? AND state = ? AND binding_digest = ?", op.ID, "generation", "submitting", op.BindingDigest).First(&current).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return commonapi.ErrConflict
				}
				return err
			}
			return nil
		}

		text, ok := item.ExecutionConfig[artifactGenerationTokensKey].(string)
		if !ok {
			return commonapi.ErrConflict
		}
		var tokens []CleanupTargetToken
		if json.Unmarshal([]byte(text), &tokens) != nil || len(tokens) < 2 {
			return commonapi.ErrConflict
		}
		for _, token := range tokens {
			id, err := token.Key.digest()
			if err != nil || token.Key.TenantID != uint(item.TenantID) {
				return commonapi.ErrConflict
			}
			var target models.CleanupTarget
			if err := tx.Where("key = ? AND operation_id = ? AND generation = ?", id, item.ExecutionID, token.Generation).First(&target).Error; err != nil {
				return err
			}
		}
		op := models.CleanupArtifactOperation{ID: item.ExecutionID, TenantID: uint(item.TenantID), Origin: "generation", ExecutionID: item.ExecutionID, Attempt: lease.Attempt, LeaseToken: lease.Token, LeaseOwner: lease.Owner, Action: "write", Snapshot: text, TargetKeys: text, State: "submitting"}
		op.BindingDigest = ArtifactOperationBinding(op)
		var existing int64
		if err := tx.Model(&models.CleanupArtifactOperation{}).Where("id = ?", op.ID).Count(&existing).Error; err != nil {
			return err
		}
		if existing != 0 {
			return commonapi.ErrConflict
		}
		if err := tx.Create(&op).Error; err != nil {
			return err
		}
		ticket.operation = &op
		return nil
	})
}

// RecordArtifactWrite appends only the exact original external outcome. It does
// not use a terminal execution/expired lease as a stopped-writer proof.
func RecordArtifactWrite(ctx context.Context, db *gorm.DB, success bool) error {
	ticket, ok := ctx.Value(artifactWriterContextKey{}).(*artifactWriterTicket)
	if !ok {
		return commonapi.ErrConflict
	}
	ticket.mu.Lock()
	defer ticket.mu.Unlock()
	if ticket.operation == nil {
		return nil
	}
	op := ticket.operation
	state := "unknown"
	if success {
		state = "succeeded"
	}
	factCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	r := db.WithContext(factCtx).Model(&models.CleanupArtifactOperation{}).Where("id = ? AND origin = ? AND binding_digest = ? AND state = ?", op.ID, "generation", op.BindingDigest, "submitting").Updates(map[string]interface{}{"state": state, "effect_at": time.Now().UTC()})
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return errors.New("artifact writer outcome is not current")
	}
	return nil
}
