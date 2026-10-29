package service

import (
	"context"
	"encoding/json"
	"sort"

	commonapi "github.com/addp/common/api"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/manager/internal/repository"
)

// Capture happens once, before enqueue and inference preparation. A queued
// request keeps these tokens across process restart; it cannot observe again.
func (s *EmbeddingService) captureCleanupTargets(ctx context.Context, tenant uint, req EmbeddingExecutionRequest, config commonmodels.JSONMap) error {
	items, err := s.resolveExecutionItems(ctx, tenant, req)
	if err != nil {
		return err
	}
	targets := make([]repository.EmbeddingCleanupTarget, 0, len(items))
	seen := map[uint]bool{}
	for _, item := range items {
		if item.ID == 0 || item.EngineID == 0 || seen[item.ID] {
			return commonapi.ErrConflict
		}
		seen[item.ID] = true
		fp := commonmodels.GenerateItemFingerprint(item.EngineID, item.FullName)
		token, err := s.vectorRepo.ObserveCleanupTarget(ctx, tenant, fp)
		if err != nil {
			return err
		}
		targets = append(targets, repository.EmbeddingCleanupTarget{ItemID: item.ID, EngineID: item.EngineID, Fingerprint: fp, Token: token})
	}
	if len(targets) == 0 {
		return commonapi.ErrConflict
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Fingerprint < targets[j].Fingerprint })
	b, err := json.Marshal(targets)
	if err != nil {
		return err
	}
	config[repository.EmbeddingCleanupTargetsKey] = string(b)
	return nil
}
