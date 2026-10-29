package repository

import (
	"context"
	"encoding/json"
	"sort"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"gorm.io/gorm"
)

const EmbeddingCleanupTargetsKey = "cleanup_embedding_targets"

type EmbeddingCleanupTarget struct {
	ItemID      uint
	EngineID    uint
	Fingerprint string
	Token       CleanupTargetToken
}

type embeddingCleanupContextKey struct{}

func ContextWithEmbeddingCleanupToken(ctx context.Context, token CleanupTargetToken) context.Context {
	return context.WithValue(ctx, embeddingCleanupContextKey{}, token)
}

func DecodeEmbeddingCleanupTargets(config commonmodels.JSONMap, tenant uint) ([]EmbeddingCleanupTarget, error) {
	text, ok := config[EmbeddingCleanupTargetsKey].(string)
	if !ok {
		return nil, commonapi.ErrConflict
	}
	var targets []EmbeddingCleanupTarget
	if json.Unmarshal([]byte(text), &targets) != nil || len(targets) == 0 {
		return nil, commonapi.ErrConflict
	}
	seen := map[uint]bool{}
	for _, t := range targets {
		if t.ItemID == 0 || t.EngineID == 0 || t.Fingerprint == "" || seen[t.ItemID] || t.Token.Key != (CleanupTargetKey{TenantID: tenant, Kind: CleanupTargetEmbedding, Identity: t.Fingerprint}) {
			return nil, commonapi.ErrConflict
		}
		seen[t.ItemID] = true
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Fingerprint < targets[j].Fingerprint })
	return targets, nil
}

func GuardEmbeddingAdmission(ctx context.Context, tx *gorm.DB, tenant uint, config commonmodels.JSONMap) error {
	targets, err := DecodeEmbeddingCleanupTargets(config, tenant)
	if err != nil {
		return err
	}
	for _, t := range targets {
		if err := GuardCleanupTargetWrite(ctx, tx, t.Token); err != nil {
			return err
		}
	}
	return nil
}

func (r *EmbeddingRepository) ObserveCleanupTarget(ctx context.Context, tenant uint, fingerprint string) (CleanupTargetToken, error) {
	return ObserveCleanupTarget(ctx, r.db, CleanupTargetKey{TenantID: tenant, Kind: CleanupTargetEmbedding, Identity: fingerprint})
}

func (r *EmbeddingRepository) CreateAdhocExecution(ctx context.Context, item *execution.TaskExecution) error {
	if item == nil || item.TenantID <= 0 || item.Module != execution.ModuleManager || item.TaskType != execution.TaskTypeEmbedding || item.SourceTaskID != nil {
		return commonapi.ErrConflict
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := GuardEmbeddingAdmission(ctx, tx, uint(item.TenantID), item.ExecutionConfig); err != nil {
			return err
		}
		return tx.Create(item).Error
	})
}
