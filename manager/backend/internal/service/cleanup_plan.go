package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	commonclient "github.com/addp/common/client"
	"github.com/addp/common/engine/selection"
	"github.com/addp/common/events"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

type managerCleanupPlan struct {
	PreviewStates []*models.PreviewState
	Embeddings    []*models.Embedding
	Tasks         []repository.CleanupTaskDefinition
	Tiles         []*models.TileCache
	Vectors       []*models.VectorMaterializedView
	Artifacts     []repository.CleanupManagedArtifact
}

// cleanupMissingSource accepts only an exact authoritative NotFound. Permission,
// transport and malformed identity errors cannot become a destructive candidate.
func (s *CleanupService) cleanupMissingSource(ctx context.Context, tenant uint, locator string, itemID uint) (bool, error) {
	if s.metaClient == nil {
		return false, errors.New("cleanup Meta authority unavailable")
	}
	c := s.metaClient.WithTenantID(tenant)
	var err error
	if itemID > 0 {
		_, err = c.GetItemByID(ctx, itemID)
	} else {
		loc, e := resourcetree.ParseURI(strings.TrimSpace(locator))
		if e != nil {
			return false, e
		}
		switch {
		case loc.ItemID != nil:
			_, err = c.GetItemByID(ctx, *loc.ItemID)
		case loc.NodeID != nil:
			_, err = c.GetNodeByID(ctx, *loc.NodeID)
		case len(loc.Path) > 0:
			_, err = c.GetItemByCatalogPath(ctx, loc.EngineID, loc.FullName())
		default:
			return false, errors.New("cleanup source identity unavailable")
		}
	}
	if err == nil {
		return false, nil
	}
	var apiErr *commonclient.TenantAPIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return true, nil
	}
	return false, err
}

func (s *CleanupService) cleanupTargetReason(ctx context.Context, tenant uint, target cleanupTaskTarget, scope map[string]interface{}, engines map[uint]bool) (string, error) {
	if target.IsEmpty() {
		return "", nil
	}
	if !s.matchesCleanupTaskTarget(target, scope) {
		return "", nil
	}
	if exact := uintFromCleanupContext(scope, "engine_id"); exact > 0 && target.EngineID == exact {
		return models.TaskBindingIssueMissingEngine, nil
	}
	if target.EngineID > 0 {
		invalid, ok := engines[target.EngineID]
		if !ok {
			if s.systemClient == nil {
				return "", errors.New("cleanup Engine authority unavailable")
			}
			engine, err := s.systemClient.WithTenantID(tenant).GetEngine(ctx, target.EngineID)
			if err != nil {
				if status, ok := commonclient.SystemAPIStatusCode(err); !ok || status != http.StatusNotFound {
					return "", err
				}
				invalid = true
			} else {
				if engine == nil || engine.ID != target.EngineID || engine.TenantID != nil && *engine.TenantID != tenant {
					return "", errors.New("cleanup Engine identity mismatch")
				}
				switch engine.LifecycleState {
				case "active":
					capabilities, err := selection.ParseCapabilities(engine.Capabilities)
					if err != nil || capabilities == nil {
						return "", errors.New("cleanup Engine capabilities unavailable")
					}
					invalid = capabilities.Storage == nil
				case "disabled", "deleting", "deleted":
					invalid = true
				default:
					return "", errors.New("cleanup Engine lifecycle unavailable")
				}
			}
			engines[target.EngineID] = invalid
		}
		if invalid {
			return models.TaskBindingIssueMissingEngine, nil
		}
	}
	if !target.VerifySource {
		return "", nil
	}
	missing, err := s.cleanupMissingSource(ctx, tenant, target.Locator, target.ItemID)
	if err != nil {
		return "", err
	}
	if missing {
		return models.TaskBindingIssueMissingSource, nil
	}
	return "", nil
}

func (s *CleanupService) buildCleanupPlan(ctx context.Context, tenant uint, scope map[string]interface{}) (*managerCleanupPlan, error) {
	if tenant == 0 {
		return nil, errors.New("cleanup Tenant required")
	}
	plan := &managerCleanupPlan{}
	engines := map[uint]bool{}
	candidate := func(locator, fp string, itemID, engineID uint) (bool, error) {
		if engineID == 0 {
			loc, err := resourcetree.ParseURI(strings.TrimSpace(locator))
			if err != nil {
				return false, err
			}
			engineID = loc.EngineID
		}
		reason, err := s.cleanupTargetReason(ctx, tenant, cleanupTaskTarget{EngineID: engineID, ItemID: itemID, ItemFingerprint: fp, Locator: locator, VerifySource: true}, scope, engines)
		return reason != "", err
	}
	if s.previewStateRepo != nil {
		values, err := s.previewStateRepo.ListPreviewStates(ctx, tenant)
		if err != nil {
			return nil, err
		}
		for _, v := range values {
			ok, err := candidate(v.Locator, v.ItemFingerprint, 0, 0)
			if err != nil {
				return nil, err
			}
			if ok {
				plan.PreviewStates = append(plan.PreviewStates, v)
			}
		}
	}
	if s.embeddingRepo != nil {
		values, err := s.embeddingRepo.ListAllEmbeddings(ctx, tenant)
		if err != nil {
			return nil, err
		}
		for _, v := range values {
			ok, err := candidate(v.Locator, v.ItemFingerprint, v.ItemID, v.EngineID)
			if err != nil {
				return nil, err
			}
			if ok {
				plan.Embeddings = append(plan.Embeddings, v)
			}
		}
	}
	if s.tileCacheSvc != nil && s.tileCacheSvc.tileCacheRepo != nil {
		values, err := s.tileCacheSvc.tileCacheRepo.ListAllTileCaches(ctx, tenant)
		if err != nil {
			return nil, err
		}
		for _, v := range values {
			ok, err := candidate(v.Locator, v.ItemFingerprint, uintPtrValue(v.ItemID), 0)
			if err != nil {
				return nil, err
			}
			if ok {
				plan.Tiles = append(plan.Tiles, v)
			}
		}
	}
	if s.optimizationSvc != nil && s.optimizationSvc.repo != nil {
		values, err := s.optimizationSvc.repo.ListAllResults(ctx, tenant)
		if err != nil {
			return nil, err
		}
		for _, v := range values {
			if v.Status == models.VectorMaterializedViewStatusAbandonedExternal {
				continue
			}
			ok, err := candidate(v.Locator, v.ItemFingerprint, uintPtrValue(v.ItemID), 0)
			if err != nil {
				return nil, err
			}
			if ok {
				plan.Vectors = append(plan.Vectors, v)
			}
		}
	}
	if s.managedArtifactRepo != nil {
		values, err := s.managedArtifactRepo.List(ctx, tenant)
		if err != nil {
			return nil, err
		}
		for _, v := range values {
			ok, err := candidate(v.Locator, v.ItemFingerprint, v.ItemID, v.SourceEngineID)
			if err != nil {
				return nil, err
			}
			if ok {
				plan.Artifacts = append(plan.Artifacts, v)
			}
		}
	}
	if s.taskDefinitionRepo != nil {
		values, err := s.taskDefinitionRepo.List(ctx, tenant)
		if err != nil {
			return nil, err
		}
		for _, v := range values {
			for _, target := range cleanupTaskTargetsFromDefinition(v) {
				reason, err := s.cleanupTargetReason(ctx, tenant, target, scope, engines)
				if err != nil {
					return nil, err
				}
				if reason != "" {
					v.CleanupReason = reason
					plan.Tasks = append(plan.Tasks, v)
					break
				}
			}
		}
	}
	return plan, nil
}

func (p *managerCleanupPlan) statistics() *ManagerCleanupStats {
	stats := &ManagerCleanupStats{PreviewStates: len(p.PreviewStates), Embeddings: len(p.Embeddings), TileCaches: len(p.Tiles), VectorMaterializedViews: len(p.Vectors), ManagedArtifacts: len(p.Artifacts), TaskDefinitions: len(p.Tasks)}
	for _, a := range p.Artifacts {
		if a.SizeBytes > 0 {
			stats.FreedBytes += a.SizeBytes
		}
	}
	return stats
}
func (p *managerCleanupPlan) impactItems() []events.CleanupImpactItem {
	var items []events.CleanupImpactItem
	add := func(ref, disposition string) {
		items = append(items, events.CleanupImpactItem{StableRef: ref, Disposition: disposition})
	}
	for _, v := range p.PreviewStates {
		add(fmt.Sprintf("manager_preview_state:%d", v.ID), events.CleanupImpactWillDelete)
	}
	for _, v := range p.Embeddings {
		add(fmt.Sprintf("manager_embedding:%d", v.ID), events.CleanupImpactWillDelete)
	}
	for _, v := range p.Tiles {
		ref := fmt.Sprintf("manager_vector_tile_cache:%d", v.ID)
		add(ref, events.CleanupImpactWillDelete)
		if v.Status == models.TileCacheStatusGenerating {
			add(ref, events.CleanupImpactRunning)
		}
	}
	for _, v := range p.Vectors {
		ref := fmt.Sprintf("manager_vector_materialized_view:%d", v.ID)
		add(ref, events.CleanupImpactExternalArtifact)
		if v.Status == models.VectorMaterializedViewStatusBuilding {
			add(ref, events.CleanupImpactRunning)
		}
	}
	for _, v := range p.Artifacts {
		ref := fmt.Sprintf("manager_%s_artifact:%d", v.TaskType, v.ID)
		add(ref, events.CleanupImpactWillDelete)
		if cleanupArtifactRunning(v.Status) {
			add(ref, events.CleanupImpactRunning)
		}
	}
	for _, v := range p.Tasks {
		ref := fmt.Sprintf("manager_%s_task:%d", v.TaskType, v.ID)
		add(ref, events.CleanupImpactWillDisable)
		if cleanupExecutionRunning(v.LastExecutionStatus) {
			add(ref, events.CleanupImpactRunning)
		}
	}
	return items
}
